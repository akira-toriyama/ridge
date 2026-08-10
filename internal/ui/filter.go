package ui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The filter bar is a furrow -q PASSTHROUGH (t-z7ye): ridge keeps only the
// raw string and the STORE answers it, so the filter bar and `furrow ls -q`
// can never disagree about what a query means. The model holds the last
// verdict as an id set; while a keystroke is in flight — or after furrow
// refuses a half-typed query — the previous verdict stays on screen, because
// a board that blanks mid-type is worse than one a keystroke behind.

// filterDebounce is how long the bar waits after a keystroke before asking
// the store. `furrow ls -q` measures 52-92ms against the real 906-task board
// (2026-08-10, spawn included), so the verdict lands ~0.2s after typing
// pauses — and never once per keystroke.
const filterDebounce = 120 * time.Millisecond

// filterTickMsg is the debounce alarm; stale seq numbers are dropped.
type filterTickMsg struct{ seq int }

// filterResultMsg carries the store's verdict for the seq-th keystroke.
type filterResultMsg struct {
	seq int
	ids []string
	err error
}

// effectiveQuery is what the store is actually asked: the typed query AND
// the slice panel's term. They are held separately so a slice switch never
// edits the user's text.
func (m *Model) effectiveQuery() string {
	if t := m.sliceTerm(); t != "" {
		return strings.TrimSpace(m.qRaw + " " + t)
	}
	return m.qRaw
}

// taskVisible is THE visibility predicate: every view (board columns, table
// rows, graph nodes) must agree with it or the same query shows different
// boards.
func (m *Model) taskVisible(t *board.Task) bool {
	if m.effectiveQuery() == "" || m.pinned[t.ID] {
		return true
	}
	if m.qMatched == nil {
		return true // no verdict yet — keep showing the last state
	}
	return m.qMatched[t.ID]
}

// applyFilter makes s the active typed query. It returns the Cmd that will
// eventually deliver the store's verdict: a debounce tick on a live store, or
// nil when the verdict was applied synchronously (fixture, or an empty
// query).
func (m *Model) applyFilter(s string) tea.Cmd {
	prev := m.curTask()
	m.qRaw = strings.TrimSpace(s)
	if m.effectiveQuery() == "" {
		// Nothing is filtering any more (typed AND slice): jump pins have
		// nothing to pin past. While a slice still narrows the board the
		// pins stay — the slice paths clear their own (selectSlice).
		m.pinned = map[string]bool{}
	}
	return m.refire(prev, true)
}

// refire re-asks the store for a verdict on the effective query. debounce is
// true for keystrokes, false for slice switches and reloads — deliberate
// gestures with no follow-up keystroke coming.
func (m *Model) refire(prev *board.Task, debounce bool) tea.Cmd {
	m.qSeq++ // fences off every in-flight tick and result
	eq := m.effectiveQuery()
	if eq == "" {
		m.qMatched, m.qErr = nil, ""
		m.refilter(prev)
		return nil
	}
	if !m.prov.Live() {
		// The fixture evaluator is in-memory and instant: answer now, so
		// -dump stays a single deterministic frame and tests need no loop.
		m.onFilterResult(filterResultMsg{seq: m.qSeq}.evaluate(m.prov, eq))
		return nil
	}
	if !debounce {
		return m.queryCmd(m.qSeq)
	}
	seq := m.qSeq
	return tea.Tick(filterDebounce, func(time.Time) tea.Msg { return filterTickMsg{seq: seq} })
}

// evaluate fills the msg with the provider's verdict, keeping the seq.
func (msg filterResultMsg) evaluate(p board.Provider, raw string) filterResultMsg {
	msg.ids, msg.err = p.Query(raw)
	return msg
}

// onFilterTick fires the store query for the NEWEST keystroke only — every
// older tick died when a newer applyFilter bumped the fence.
func (m *Model) onFilterTick(msg filterTickMsg) tea.Cmd {
	if msg.seq != m.qSeq {
		return nil
	}
	return m.queryCmd(msg.seq)
}

// requery re-runs the active query NOW, without the debounce — after a
// reload the board changed under the matched set, so the verdict is stale.
func (m *Model) requery() tea.Cmd {
	if m.effectiveQuery() == "" {
		return nil
	}
	return m.refire(m.curTask(), false)
}

func (m *Model) queryCmd(seq int) tea.Cmd {
	prov, raw := m.prov, m.effectiveQuery()
	return func() tea.Msg {
		return filterResultMsg{seq: seq}.evaluate(prov, raw)
	}
}

// onFilterResult lands a verdict. Stale verdicts are dropped whole: applying
// an old result set over a newer query would show the WRONG board while the
// bar shows the newer text.
func (m *Model) onFilterResult(msg filterResultMsg) {
	if msg.seq != m.qSeq {
		return
	}
	if m.inflight || len(m.pending) > 0 || m.rollingBack {
		// The verdict was computed from store truth that predates the queued
		// optimistic writes — applying it would blink the user's own edit off
		// the board. The post-drain reconcile requeries without a debounce.
		// The rollback window counts as a queued write even though the queue
		// is empty: the board is showing refused state, and the rollback
		// re-read's own requery supplies the fresh verdict.
		return
	}
	if m.mode == modeMove {
		// A verdict reshaping the columns mid-move silently rewrites where
		// the aimed slot lands — the status line says one slot, the commit
		// writes another (t-74y3). Hold it; the move's exit applies it.
		m.heldVerdict = &msg
		return
	}
	m.applyVerdict(msg)
}

// applyVerdict lands a seq-checked verdict on the board.
func (m *Model) applyVerdict(msg filterResultMsg) {
	if msg.err != nil {
		// All-or-nothing refusal (furrow exit 2): keep the last good verdict
		// on screen and say why. A half-typed `value:>` must not blank the
		// board — the loudest possible way to be non-fatal.
		m.qErr = msg.err.Error()
		return
	}
	m.qErr = ""
	set := make(map[string]bool, len(msg.ids))
	for _, id := range msg.ids {
		set[id] = true
	}
	m.qMatched = set
	m.refilter(m.curTask())
}

// releaseHeldVerdict applies a verdict that landed mid-move — called on every
// move-mode exit, AFTER the commit consumed its drop slot. It re-enters
// onFilterResult so the queued-writes guard still applies: a commit that just
// queued a persist drops the hold and lets the post-drain reconcile requery.
// A stale hold (a newer keystroke re-queried meanwhile) drops like any stale
// verdict.
func (m *Model) releaseHeldVerdict() {
	v := m.heldVerdict
	m.heldVerdict = nil
	if v == nil || v.seq != m.qSeq {
		return
	}
	m.onFilterResult(*v)
}

// refilter recomputes the visible board and keeps the selection where the
// user left it.
func (m *Model) refilter(prev *board.Task) {
	m.recompute()
	if prev != nil {
		m.selectID(prev.ID, false)
	}
}

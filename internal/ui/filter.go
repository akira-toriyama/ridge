package ui

import (
	"strings"
	"time"

	"charm.land/bubbles/v2/key"
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
	// why is furrow's reason list per id, only when the verdict came from
	// the revisit lens; nil for an ls -q verdict.
	why map[string][]board.RevisitReason
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

// lensOn reports whether SOMETHING narrows the board: a query (typed or
// sliced) or the revisit lens. Every "is anything filtering" test reads this
// rather than the query alone, because the lens narrows with an empty query.
func (m *Model) lensOn() bool {
	return m.effectiveQuery() != "" || m.revisitOn
}

// taskVisible is THE visibility predicate: every view (board columns, table
// rows, graph nodes) must agree with it or the same query shows different
// boards.
func (m *Model) taskVisible(t *board.Task) bool {
	if !m.lensOn() || m.pinned[t.ID] {
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
	if !m.lensOn() {
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
	if !m.lensOn() {
		m.qMatched, m.qErr, m.revisitWhy = nil, "", nil
		m.refilter(prev)
		return nil
	}
	if !m.prov.Live() {
		// The fixture evaluator is in-memory and instant: answer now, so
		// -dump stays a single deterministic frame and tests need no loop.
		m.onFilterResult(filterResultMsg{seq: m.qSeq}.evaluate(m.prov, eq, m.revisitOn))
		return nil
	}
	if !debounce {
		return m.queryCmd(m.qSeq)
	}
	seq := m.qSeq
	return tea.Tick(filterDebounce, func(time.Time) tea.Msg { return filterTickMsg{seq: seq} })
}

// evaluate fills the msg with the provider's verdict, keeping the seq. With
// the lens on the verdict is `revisit -q`'s: the flagged tasks that ALSO
// match raw, reasons attached — furrow ANDs the two, so ridge never
// intersects verdicts of its own.
func (msg filterResultMsg) evaluate(p board.Provider, raw string, revisit bool) filterResultMsg {
	if !revisit {
		msg.ids, msg.err = p.Query(raw)
		return msg
	}
	rows, err := p.Revisit(raw)
	if err != nil {
		msg.err = err
		return msg
	}
	msg.why = make(map[string][]board.RevisitReason, len(rows))
	for _, r := range rows {
		msg.ids = append(msg.ids, r.ID)
		msg.why[r.ID] = r.Reasons
	}
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
	if !m.lensOn() {
		return nil
	}
	return m.refire(m.curTask(), false)
}

func (m *Model) queryCmd(seq int) tea.Cmd {
	prov, raw, revisit := m.prov, m.effectiveQuery(), m.revisitOn
	return func() tea.Msg {
		return filterResultMsg{seq: seq}.evaluate(prov, raw, revisit)
	}
}

// onFilterResult lands a verdict. Stale verdicts are dropped whole: applying
// an old result set over a newer query would show the WRONG board while the
// bar shows the newer text.
func (m *Model) onFilterResult(msg filterResultMsg) {
	if msg.seq != m.qSeq {
		return
	}
	if m.queueBusy() || m.rollingBack {
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
	m.revisitWhy = msg.why
	m.refilter(m.curTask())
}

// toggleRevisit is the `f` key. The note says what the lens shows only on
// the way in: on the way out the filter row's chip vanishes, which is the
// same statement.
func (m *Model) toggleRevisit() tea.Cmd {
	on := !m.revisitOn
	if on {
		m.note("revisit lens on — only what furrow revisit flags; the peek says why")
	}
	return m.setRevisit(on)
}

// setRevisit is the note-free half of the toggle, and the one -revisit uses
// from the constructor: the read-only warning is set once per session and
// restored by nothing, so a startup note would erase it (the -roadmap
// precedent — startRoadmap exists for the same reason).
//
// Turning the lens on re-asks the store at once (a deliberate gesture, like a
// slice switch — no debounce). Turning it off tears down everything the lens
// owned BEFORE the refire: the reasons (a refused re-query keeps the last good
// verdict, so applyVerdict would never clear them) and, when nothing else
// narrows the board, the jump pins — applyFilter's own rule, which the toggle
// bypasses. A stale reason line under a vanished chip, and "+1 pinned" over
// an unfiltered board, were both measured before this.
func (m *Model) setRevisit(on bool) tea.Cmd {
	m.revisitOn = on
	if !on {
		m.revisitWhy = nil
		if !m.lensOn() {
			m.pinned = map[string]bool{}
		}
	}
	return m.refire(m.curTask(), false)
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

func (m *Model) onFilterKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.ForceQuit):
		// `q` types into the filter; ctrl+c stays a way out (raw mode hands
		// it to us as an ordinary keystroke — nobody else will quit for us).
		return m.quitOrFlush()
	case key.Matches(msg, m.keys.Cancel):
		m.mode = modeNormal
		m.ti.Blur()
		m.ti.SetValue(m.qRaw) // discard the in-progress edit; the verdict is current
		return nil
	case key.Matches(msg, m.keys.Commit):
		m.mode = modeNormal
		m.ti.Blur()
		return m.applyFilter(m.ti.Value())
	}
	var c tea.Cmd
	m.ti, c = m.ti.Update(msg)
	return tea.Batch(c, m.applyFilter(m.ti.Value())) // live filtering as you type
}

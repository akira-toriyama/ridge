package ui

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The optimistic-write machinery.
//
// Every mutation is applied to the in-memory board FIRST, on the UI thread —
// the render after Update already shows it — and the store write runs behind
// it inside a tea.Cmd. Measured against the real 906-task store that write is
// 85-115ms, and 280ms when a lane respaces: blocking the event loop for it
// would make every drop a visible hitch, which is why this queue exists.
//
// Exactly one write is in flight at a time. Ordering is not a nicety: two
// reorders in the same lane anchor on neighbour ids ("--before t-x"), and
// landing them out of order would invert the second gesture's intent.
type persistOp struct {
	label string
	run   func() (renumbered []string, err error)
	// addedID is non-nil only for a quick add: the store invents the id, so
	// run writes it here and onPersistDone lands the cursor on it. Everything
	// else about an add rides the same strictly-serial queue as the other
	// writes — a second furrow process mutating the same git-backed store is
	// exactly what the queue exists to prevent.
	addedID *string
}

type persistDoneMsg struct {
	label      string
	renumbered []string
	ms         int
	err        error
}

// reloadDoneMsg reports an async store re-read: an explicit reload, the
// post-queue reconcile, a sync, or a rollback. label is the note to show;
// "" is the silent reconcile. rollback marks the re-read that IS the
// rollback of a failed write — its failure must say "your edit is unsaved",
// not the generic "reload failed".
type reloadDoneMsg struct {
	label    string
	ms       int
	rollback bool
	err      error
}

// enqueuePersist queues one store write whose effect is already on the board,
// and starts the queue when it is idle. Nil when a write is in flight — the
// queue drains itself from onPersistDone.
func (m *Model) enqueuePersist(label string, run func() ([]string, error)) tea.Cmd {
	m.pending = append(m.pending, persistOp{label: label, run: run})
	if m.inflight {
		return nil
	}
	return m.firePersist()
}

func (m *Model) firePersist() tea.Cmd {
	m.inflight = true
	op := m.pending[0]
	return func() tea.Msg {
		start := time.Now()
		renumbered, err := op.run()
		return persistDoneMsg{label: op.label, renumbered: renumbered,
			ms: int(time.Since(start).Milliseconds()), err: err}
	}
}

// onPersistDone pops the queue: the next write, or — once drained — one
// silent reconcile so the board converges on the store's own truth (respaced
// priorities, close stamps, epic progress): a re-read on a live provider, a
// requery on the fixture (whose board mutates in place, so only the filter
// verdict can be stale).
func (m *Model) onPersistDone(msg persistDoneMsg) tea.Cmd {
	op := m.pending[0]
	m.inflight = false
	m.pending = m.pending[1:]
	m.lastPersist = fmt.Sprintf("%s %dms", msg.label, msg.ms)
	if msg.err != nil {
		// The optimistic edit lied. Everything queued behind it computed its
		// anchors from a board state the store never reached, so drop it all
		// and re-read: the reload IS the rollback. A quit waiting on the
		// flush is cancelled — exiting on top of a failed write would make
		// the loss silent.
		flushed := m.pending
		m.pending = nil
		m.quitting = false

		// The flushed tail is named, not just counted: a queued quick add
		// has NO optimistic effect, so nothing on the board even hints that
		// the typed title was thrown away — the message is its only trace.
		loss := ""
		if n := len(flushed); n > 0 {
			labels := make([]string, n)
			for i, f := range flushed {
				labels[i] = f.label
			}
			loss = fmt.Sprintf(" · dropped %d queued: %s", n, strings.Join(labels, ", "))
		}

		// Roll back only what was optimistically applied. Adds apply
		// nothing, so a failure chain of adds alone needs no re-read — and
		// claiming "unsaved edit" over one would be a lie.
		needRollback := op.addedID == nil
		for _, f := range flushed {
			if f.addedID == nil {
				needRollback = true
			}
		}
		if !needRollback {
			// No re-read is coming, so a cursor jump parked by an earlier
			// successful add would fire at some unrelated future reload.
			m.selectAfterReload = ""
			m.fail("%s: %v%s", msg.label, msg.err, loss)
			return nil
		}
		m.fail("%s: %v%s — rolling back", msg.label, msg.err, loss)
		return m.rollbackReloadCmd()
	}
	if op.addedID != nil && *op.addedID != "" {
		id := *op.addedID
		if m.prov.Live() {
			// The reconcile below (or the drain's, when more writes are
			// queued) delivers the card; land the cursor on it then.
			m.selectAfterReload = id
		} else {
			m.reload()
			m.selectID(id, true)
		}
		m.note("added %s", id)
	}
	if len(m.pending) > 0 {
		return m.firePersist()
	}
	if m.quitting {
		return tea.Quit
	}
	if m.prov.Live() {
		return m.reloadCmd("")
	}
	return m.requery()
}

// quitOrFlush is every quit key's guard: leaving with writes still queued
// would race the store (the child process would finish, but nothing would be
// left to report a failure), so quit waits for the drain instead — saying so,
// because with the store's 15s timeout in the worst case a silent wait reads
// as a hang. Only a FAILED write cancels the quit.
func (m *Model) quitOrFlush() tea.Cmd {
	if m.inflight || len(m.pending) > 0 {
		if !m.quitting {
			m.quitting = true
			// APPENDED to the gesture's own report, not replacing it — the
			// user needs both "what my last edit did" and "why q did not
			// quit yet".
			m.status += " — quitting once the queued writes land"
		}
		return nil
	}
	return tea.Quit
}

// enqueueAdd queues a quick add. It rides the same queue as every other
// write (never a bare Cmd racing the queue's own furrow process), but unlike
// them it applies nothing optimistically — the store invents the id, so the
// card appears at the reconcile that follows the drain.
func (m *Model) enqueueAdd(title string, opts board.AddOptions) tea.Cmd {
	prov := m.prov
	id := new(string)
	m.pending = append(m.pending, persistOp{
		label:   "add " + title,
		addedID: id,
		run: func() ([]string, error) {
			got, err := prov.Add(title, opts)
			*id = got
			return nil, err
		},
	})
	if m.inflight {
		return nil
	}
	return m.firePersist()
}

// persistPlacement queues the store write for id's already-applied placement,
// anchored on its neighbours in the FULL destination lane as it stands after
// the local apply — the anchors are computed on the UI thread precisely so the
// background write never has to read a board that is still being edited.
func (m *Model) persistPlacement(id, lane string) tea.Cmd {
	before, after := m.b.Neighbors(id)
	return m.enqueuePersist("move "+id, func() ([]string, error) {
		return m.prov.PersistMove(id, lane, before, after)
	})
}

func (m *Model) reloadCmd(label string) tea.Cmd {
	return m.reloadCmdOpts(label, false)
}

// rollbackReloadCmd is the re-read that undoes a failed optimistic write.
func (m *Model) rollbackReloadCmd() tea.Cmd {
	return m.reloadCmdOpts("", true)
}

func (m *Model) reloadCmdOpts(label string, rollback bool) tea.Cmd {
	prov := m.prov
	return func() tea.Msg {
		start := time.Now()
		err := prov.Reload()
		return reloadDoneMsg{label: label, rollback: rollback,
			ms: int(time.Since(start).Milliseconds()), err: err}
	}
}

// syncCmd runs the store's git sync and re-reads on success, as one background
// step — the pull may have brought other machines' writes in.
func (m *Model) syncCmd() tea.Cmd {
	prov := m.prov
	return func() tea.Msg {
		start := time.Now()
		err := prov.Sync()
		if err == nil {
			err = prov.Reload()
		}
		return reloadDoneMsg{label: "synced", ms: int(time.Since(start).Milliseconds()), err: err}
	}
}

func (m *Model) onReloadDone(msg reloadDoneMsg) tea.Cmd {
	label := msg.label
	if label == "" {
		label = "reload"
	}
	if msg.err != nil {
		if msg.rollback {
			// The one reload that must not fail quietly: until a re-read
			// lands, the board keeps showing the write the store refused.
			m.fail("rollback re-read failed — the board may show an unsaved edit; press r to retry (%v)", msg.err)
			return nil
		}
		m.fail("%s: %v", label, msg.err)
		return nil
	}
	if m.inflight || len(m.pending) > 0 {
		// A write landed behind this snapshot. Applying it now would yank the
		// user's newer optimistic edits back; the queue's own reconcile will
		// re-read once it drains.
		return nil
	}
	m.reload()
	if msg.label != "" {
		m.note("%s · %dms", msg.label, msg.ms)
	}
	if id := m.selectAfterReload; id != "" {
		m.selectAfterReload = ""
		// Pin past any active filter: a card you just created must be under
		// the cursor even when the filter would hide it.
		m.selectID(id, true)
	}
	// The board changed under the matched set: ask the store for a fresh
	// verdict, no debounce — this is a reload, not a keystroke.
	return m.requery()
}

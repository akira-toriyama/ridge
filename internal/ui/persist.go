package ui

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"
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
}

type persistDoneMsg struct {
	label      string
	renumbered []string
	ms         int
	err        error
}

// reloadDoneMsg reports an async store re-read: an explicit reload, the
// post-queue reconcile, or a sync. label is the note to show; "" is the
// silent reconcile.
type reloadDoneMsg struct {
	label string
	ms    int
	err   error
}

// enqueuePersist queues one store write whose effect is already on the board,
// and starts the queue when it is idle. Nil when a write is in flight — the
// queue drains itself from onPersistDone.
func (m *Model) enqueuePersist(label string, run func() ([]string, error)) tea.Cmd {
	if m.rollingBack {
		// The board is showing state the store refused; this write's indices
		// and anchors were computed against that lie. Refuse it — the
		// rollback re-read about to land will also revert its local half.
		m.fail("%s dropped — the store refused the last write, rolling back", label)
		return nil
	}
	m.pending = append(m.pending, persistOp{label: label, run: run})
	if m.inflight || m.addInFlight {
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

// onPersistDone pops the queue: the next write, or — once drained, on a live
// provider — one silent reconcile re-read so the board converges on the
// store's own truth (respaced priorities, close stamps, epic progress).
func (m *Model) onPersistDone(msg persistDoneMsg) tea.Cmd {
	m.inflight = false
	m.pending = m.pending[1:]
	m.lastPersist = fmt.Sprintf("%s %dms", msg.label, msg.ms)
	if msg.err != nil {
		// The optimistic edit lied. Everything queued behind it computed its
		// anchors from a board state the store never reached, so drop it all
		// and re-read: the reload IS the rollback. Until it lands the board
		// keeps showing the refused state, so `rollingBack` refuses new
		// writes — otherwise one keystroke in that window both preempts the
		// re-read and can address the wrong store row (t-74y3). A quit
		// waiting on the flush is cancelled — exiting on top of a failed
		// write would make the loss silent.
		m.pending = nil
		m.quitting = false
		m.rollingBack = true
		m.fail("%s: %v — rolling back", msg.label, msg.err)
		return m.reloadCmd("")
	}
	if len(m.pending) > 0 {
		return m.firePersist()
	}
	if len(m.deferredAdds) > 0 {
		return m.fireAdd()
	}
	if m.quitting {
		return tea.Quit
	}
	if m.prov.Live() {
		return m.reloadCmd("")
	}
	return nil
}

// quitOrFlush is every quit key's guard: leaving with writes still queued
// would race the store (the child process would finish, but nothing would be
// left to report a failure), so quit waits for the drain instead. Silently —
// the drain is a few hundred ms at worst and the status line keeps the last
// gesture's report; only a FAILED write speaks, and it cancels the quit.
func (m *Model) quitOrFlush() tea.Cmd {
	if m.inflight || len(m.pending) > 0 || m.addInFlight || len(m.deferredAdds) > 0 {
		m.quitting = true
		return nil
	}
	return tea.Quit
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
	prov := m.prov
	return func() tea.Msg {
		start := time.Now()
		err := prov.Reload()
		return reloadDoneMsg{label: label, ms: int(time.Since(start).Milliseconds()), err: err}
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
		if m.rollingBack {
			// The rollback re-read itself failed: the board still shows what
			// the store refused. Say so loudly rather than gating writes
			// forever behind a re-read that may never succeed.
			m.rollingBack = false
			m.fail("%s: %v — rollback re-read failed, the board may not match the store (press r)", label, msg.err)
			return nil
		}
		m.fail("%s: %v", label, msg.err)
		return nil
	}
	if m.rollingBack {
		// The rollback outranks the skip guard below: nothing can be queued
		// behind it (enqueuePersist refuses while rollingBack), and skipping
		// it is exactly how a failed write stayed on screen (t-74y3).
		m.rollingBack = false
	} else if m.inflight || len(m.pending) > 0 || m.addInFlight {
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
		// Pin past any active filter: a card you just created must be under
		// the cursor even when the filter would hide it. Cleared only once
		// the card was actually FOUND — an unrelated reload landing first
		// (an explicit `r`, a sync) must not consume the pending selection
		// while the create's own snapshot is still on its way (t-74y3).
		if m.selectID(id, true) {
			m.selectAfterReload = ""
		}
	}
	var next tea.Cmd
	if len(m.deferredAdds) > 0 {
		next = m.fireAdd()
	}
	// The board changed under the matched set: ask the store for a fresh
	// verdict, no debounce — this is a reload, not a keystroke.
	return tea.Batch(m.requery(), next)
}

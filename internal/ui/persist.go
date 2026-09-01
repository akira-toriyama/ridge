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
	// noLocal marks a STORE-FIRST write: nothing was applied to the board, so
	// the store's answer is the only place the change exists. Two consequences,
	// both handled in onPersistDone: a refusal needs no rollback (there is no
	// optimistic half to undo), and a SUCCESS needs a re-read, because the
	// board cannot show what it was never told. The quick add and the whole
	// epic family are the members.
	noLocal bool
	// addedID is non-nil only for a quick add: the store invents the id, so
	// run writes it here and onPersistDone lands the cursor on it. Everything
	// else about an add rides the same strictly-serial queue as the other
	// writes — a second furrow process mutating the same git-backed store is
	// exactly what the queue exists to prevent.
	addedID *string
	// The submission the add carried, kept so a store refusal can hand the
	// typed text back (reopen the modal) instead of eating it (t-74y3).
	// addRaw is the line as TYPED, inline tokens and all — the reopened modal
	// restores it, so a due form furrow refused comes back editable; addTitle
	// is the parsed title, which is what labels and failure notes quote.
	// addOpts is the INHERITED context only, pre-apply: the reopened modal
	// re-parses addRaw live, and Draft is the one field apply() ORs instead
	// of assigning, so storing the composed opts made a typed is:draft
	// unclearable after a refusal — delete the token, the chip stays (found
	// by review).
	addRaw   string
	addTitle string
	addOpts  board.AddOptions
	// The same contract for the new-box modal: an epic add is store-first, so
	// a refused one leaves no trace on the board at all — the reopened modal
	// is the only thing that keeps the typed title alive.
	epicAddTitle string
	epicAddRepo  string
	// note carries prose the WRITE computed — `epic deactivate`'s
	// previous-active suggestion, which furrow derives from its activation log
	// and ridge cannot. run fills it on the queue's goroutine and the UI thread
	// reads it only after persistDoneMsg has crossed the channel back, the same
	// handoff addedID uses.
	note *string
}

type persistDoneMsg struct {
	label string
	ms    int
	err   error
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

// refuseWhileRollingBack refuses a gesture before it commits anything the
// window cannot take back. enqueuePersist and enqueueStoreFirstOp both refuse
// too, but only after the caller has already mutated m.b or closed a modal over
// hand-typed text, and those outlive the message: the mutation stays on screen
// until the rollback re-read lands, and permanently if that re-read fails,
// since onReloadDone clears the window without restoring the board.
//
// The status line is not what breaks — the Done handler's note() is overwritten
// by the queue's fail() inside the same Update, and applyCheck never notes at
// all, so no success is ever rendered (measured). The board is.
//
// Same wording and the same apply/refused debug event those two funnels emit,
// one layer earlier: the trail a "my edit vanished" report is read from must
// not lose the refusal because it moved.
func (m *Model) refuseWhileRollingBack(label string) bool {
	if !m.rollingBack {
		return false
	}
	// TestDebugLogRecordsFailureAndRollbackRefusal reads this sequence.
	m.dbg.event("apply", "refused", map[string]any{"label": label, "why": "rolling-back"})
	m.fail("%s dropped — the store refused the last write, rolling back", label)
	return true
}

// enqueuePersist queues one store write whose effect is already on the board,
// and starts the queue when it is idle. Nil when a write is in flight — the
// queue drains itself from onPersistDone.
func (m *Model) enqueuePersist(label string, run func() ([]string, error)) tea.Cmd {
	if m.rollingBack {
		// The board is showing state the store refused; this write's indices
		// and anchors were computed against that lie. Refuse it — the
		// rollback re-read about to land will also revert its local half.
		m.dbg.event("apply", "refused", map[string]any{"label": label, "why": "rolling-back"})
		m.fail("%s dropped — the store refused the last write, rolling back", label)
		return nil
	}
	m.dbg.event("apply", "enqueue", map[string]any{"label": label})
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
		// The store's respace report is discarded here on purpose: the board
		// has already been respaced locally by MoveTo, whose own report is
		// what the status line quotes. The port still returns it — it is the
		// documented shape of a furrow write — but this queue has no use for
		// it, and carrying it in the message only looked like it did.
		_, err := op.run()
		return persistDoneMsg{label: op.label,
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
		labels := make([]string, len(flushed))
		for i, f := range flushed {
			labels[i] = f.label
		}
		loss := ""
		if len(labels) > 0 {
			loss = fmt.Sprintf(" · dropped %d queued: %s", len(labels), strings.Join(labels, ", "))
		}
		m.dbg.event("persist", "fail", map[string]any{
			"label": msg.label, "ms": msg.ms, "err": msg.err.Error(), "dropped": labels,
		})

		// Roll back only what was optimistically applied. Store-first writes
		// apply nothing, so a failure chain of those alone needs no re-read —
		// and claiming "unsaved edit" over one would be a lie.
		needRollback := !op.noLocal
		for _, f := range flushed {
			if !f.noLocal {
				needRollback = true
			}
		}
		// A refused add hands its typed title back by reopening the modal —
		// unless the user has already moved on to another mode (see
		// reopenRefusedAdd). Batched with the rollback so neither is lost.
		var reopen tea.Cmd
		if op.addedID != nil {
			reopen = m.reopenRefusedAdd(op)
		}
		if op.epicAddTitle != "" {
			reopen = m.reopenRefusedEpicAdd(op)
		}
		if !needRollback {
			if m.unreadLanded {
				// An earlier write LANDED and the board has not re-read it.
				// Nothing was applied optimistically here, so no ROLLBACK
				// re-read is coming — and without a plain one a store-first
				// write stays invisible until the next `r`. Deliberately a
				// plain reload, not rollbackReloadCmd: its failure message
				// claims the board may show an unsaved edit, which would be a
				// lie. The pending cursor jump is deliberately NOT cleared —
				// a re-read is on its way, so the add it belongs to can still
				// be delivered. The flag is cleared by that re-read, not here.
				m.fail("%s: %v%s", msg.label, msg.err, loss)
				return tea.Batch(m.reloadCmd(""), reopen)
			}
			// No re-read is coming, so a cursor jump parked by an earlier
			// successful add would fire at some unrelated future reload.
			m.selectAfterReload = ""
			m.fail("%s: %v%s", msg.label, msg.err, loss)
			return reopen
		}
		// Until the re-read lands the board keeps showing the refused state,
		// so `rollingBack` refuses new writes — otherwise one keystroke in
		// that window both preempts the re-read and can address the wrong
		// store row (t-74y3). The rollback re-read is a full one, so it also
		// delivers whatever had landed — and clears unreadLanded when it applies.
		m.rollingBack = true
		m.fail("%s: %v%s — rolling back", msg.label, msg.err, loss)
		return tea.Batch(m.rollbackReloadCmd(), reopen)
	}
	m.dbg.event("persist", "done", map[string]any{"label": msg.label, "ms": msg.ms})
	if m.prov.Live() {
		// The store moved and the board has not re-read it (see
		// Model.unreadLanded). Never set on the fixture: there the board IS the
		// store, the store-first branch below re-reads synchronously, and
		// prov.Reload() is the DISCARD operation — firing it later would throw
		// the session's own epic edits away.
		m.unreadLanded = true
	}
	if op.noLocal {
		// The write landed and the board was never told. A live store converges
		// on the reconcile at the end of the drain (or on the refusal path's
		// re-read); the fixture has no reconcile there — it requeries instead —
		// so it re-reads right here, the shape the quick add has always used.
		if m.prov.Live() {
			m.storeFirstUnread = true
		} else {
			m.reload()
		}
		// The gesture's own note said "waiting for furrow"; replace it now that
		// the wait is over, and carry whatever prose the write computed.
		if op.note != nil && *op.note != "" {
			m.note("%s · %s", op.label, *op.note)
		} else if op.addedID == nil {
			m.note("%s", op.label)
		}
	}
	if op.addedID != nil && *op.addedID != "" {
		id := *op.addedID
		if m.prov.Live() {
			// The reconcile below (or the drain's, when more writes are
			// queued) delivers the card; land the cursor on it then.
			m.selectAfterReload = id
		} else {
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
// as a hang. Only a FAILED write cancels the quit — and the held body's
// refused replay (applyEditorBody), which removes from the drain the very
// write the quit was armed on.
func (m *Model) quitOrFlush() tea.Cmd {
	if m.inflight || len(m.pending) > 0 || m.heldBody != nil {
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
func (m *Model) enqueueAdd(title, raw string, inherited, opts board.AddOptions) tea.Cmd {
	if m.rollingBack {
		// Backstop only — onAddKey refuses first and keeps the modal (and
		// the typed title) open. A future caller must not be able to slip a
		// write into the window just because it skipped the modal.
		m.dbg.event("apply", "refused", map[string]any{"label": "add " + title, "why": "rolling-back"})
		m.fail("add %q dropped — the store refused the last write, rolling back", title)
		return nil
	}
	m.dbg.event("apply", "enqueue", map[string]any{"label": "add " + title, "storeFirst": true})
	prov := m.prov
	id := new(string)
	m.pending = append(m.pending, persistOp{
		label:    "add " + title,
		noLocal:  true,
		addedID:  id,
		addRaw:   raw,
		addTitle: title,
		addOpts:  inherited,
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

// enqueueStoreFirstOp queues a write whose effect exists ONLY in the store —
// the epic family (board.Provider's epic methods) and nothing else. It shares
// the queue with the optimistic writes so ordering and the quit-flush still
// hold; what differs is that a refusal rolls nothing back and a success has
// to be re-read before the board can show it (see persistOp.noLocal). The op
// arrives pre-shaped so a write can carry its own payload — the epic add's
// typed title (epicAddTitle) rides here.
func (m *Model) enqueueStoreFirstOp(op persistOp) tea.Cmd {
	// Stamped, never trusted from the caller: an op through this entrance is
	// store-first by definition, and one that forgot the flag would be rolled
	// back as if it had an optimistic half.
	op.noLocal = true
	if m.rollingBack {
		// The board is showing state the store refused. This write's SUBJECT
		// (an epic id) survives a rollback, but letting it through would
		// preempt the re-read that is the rollback — the t-74y3 window.
		m.dbg.event("apply", "refused", map[string]any{"label": op.label, "why": "rolling-back"})
		m.fail("%s dropped — the store refused the last write, rolling back", op.label)
		return nil
	}
	m.dbg.event("apply", "enqueue", map[string]any{"label": op.label, "storeFirst": true})
	m.pending = append(m.pending, op)
	if m.inflight {
		return nil
	}
	return m.firePersist()
}

// storeFirstInflight reports whether a store-first write is queued or running.
// A store-first gesture shows nothing until the write lands (~85-115ms plus the
// re-read), so the surface that issues one refuses a repeat rather than let an
// impatient second keypress aim at values the board has not updated yet
// (epicmode.go's refuseWhileWriting has the reasoning).
// The in-flight op is still m.pending[0] — firePersist reads it without
// popping and onPersistDone pops afterwards — so one loop covers both.
func (m *Model) storeFirstInflight() bool {
	for _, op := range m.pending {
		if op.noLocal {
			return true
		}
	}
	return false
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
		m.dbg.event("persist", "reloadfail", map[string]any{
			"label": label, "ms": msg.ms, "rollback": msg.rollback, "err": msg.err.Error(),
		})
		if msg.rollback {
			// The one reload that must not fail quietly: until a re-read
			// lands, the board keeps showing the write the store refused.
			// CLEAR the window even so — gating every write forever behind
			// a re-read that may never succeed trades a lying board for a
			// dead one; the message hands the user the retry.
			m.rollingBack = false
			// The held $EDITOR body still applies: its payload is complete
			// (id + full text, no indices), and dropping it here would lose
			// hand-typed work to a reload that merely failed. Released
			// BEFORE the failure message so press-r is what the user is
			// left reading, not the replay's own note.
			heldBody := m.releaseHeldBody()
			m.fail("rollback re-read failed — the board may show an unsaved edit; press r to retry (%v)", msg.err)
			return heldBody
		}
		m.fail("%s: %v", label, msg.err)
		return nil
	}
	if m.inflight || len(m.pending) > 0 {
		// A write landed behind this snapshot. Applying it now would yank the
		// user's newer optimistic edits back; the queue's own reconcile will
		// re-read once it drains. (A rollback re-read never skips here: while
		// rollingBack every write path refuses, so the queue stays empty by
		// construction until the window closes.)
		//
		// Recorded because a skipped snapshot is exactly the kind of ghost a
		// "the board didn't update" report hinges on — nothing on screen
		// distinguishes it from a reload that never ran.
		m.dbg.event("persist", "reloadskip", map[string]any{"label": label, "ms": msg.ms})
		return nil
	}
	m.reload()
	m.dbg.event("persist", "reload", map[string]any{"label": label, "ms": msg.ms, "rollback": msg.rollback})
	// The board now shows the store's own truth, whichever reload delivered
	// it — the rollback window closes and nothing is left unread. Cleared HERE
	// rather than where the reload was fired: a reload that never applies (the
	// in-flight guard above, or an error) still owes the board a re-read.
	m.rollingBack = false
	m.unreadLanded, m.storeFirstUnread = false, false
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
	// A held $EDITOR body replays now that the window is closed — after the
	// reload's own note, so "body updated" is what survives on the line.
	heldBody := m.releaseHeldBody()
	// The board changed under the matched set: ask the store for a fresh
	// verdict, no debounce — this is a reload, not a keystroke.
	return tea.Batch(heldBody, m.requery())
}

// releaseHeldBody replays a $EDITOR result that waited out the rollback
// window. Nil when nothing was held.
func (m *Model) releaseHeldBody() tea.Cmd {
	hb := m.heldBody
	if hb == nil {
		return nil
	}
	m.heldBody = nil
	return m.applyEditorBody(*hb)
}

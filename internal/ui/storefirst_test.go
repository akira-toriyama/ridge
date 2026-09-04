package ui

import (
	"errors"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
)

// A STORE-FIRST write (persistOp.noLocal — the quick add and the epic family)
// applies nothing to the board, so the store's answer is the only place its
// effect exists. That makes the refusal path subtler than the optimistic one:
// there is nothing to roll back, but a write that already LANDED still has to be
// re-read, or it stays invisible until the user presses `r`.
//
// This is a pre-existing hole, not one the epic family introduced: it was
// reachable with two quick adds long before, where the cost was a created card
// that never appeared. Store-first epic writes make it the normal path, so the
// fix belongs here.

// storeFirst shapes the minimal op the way epicWriteNoting does, minus the
// refusal gate — these tests aim at the queue itself. noLocal is stamped by
// the entrance.
func storeFirst(m *Model, label string, run func() error) tea.Cmd {
	return m.enqueueStoreFirstOp(persistOp{label: label,
		run: func() ([]string, error) { return nil, run() }})
}

func storeFirstModel(t *testing.T) (*Model, *scriptedProvider) {
	t.Helper()
	p := newScriptedProvider(scriptedEpicBoard)
	m := New(p, Options{})
	m.w, m.h = 240, 50
	m.recompute()
	m.relayout()
	return m, p
}

// One store-first write lands, the next is refused: the queue must re-read, and
// it must NOT claim the board is showing an unsaved edit (nothing was applied).
func TestStoreFirstLandedWriteIsReReadWhenALaterOneIsRefused(t *testing.T) {
	m, p := storeFirstModel(t)
	p.epicErr, p.epicFailAt = errors.New("epic-active-clash"), 2

	goal := "先に着地するゴール"
	cmd1 := storeFirst(m, "epic set e-one", func() error {
		return p.EpicSet("e-one", board.EpicPatch{Goal: &goal})
	})
	cmd2 := storeFirst(m, "epic activate e-two", func() error {
		return p.EpicActivate("e-two", "")
	})
	if cmd1 == nil {
		t.Fatal("the first store-first write did not fire")
	}
	if cmd2 != nil {
		t.Fatal("the second must queue behind the in-flight one")
	}

	next := m.onPersistDone(cmd1().(persistDoneMsg))
	if next == nil {
		t.Fatal("draining must fire the queued write")
	}
	if !m.unreadLanded {
		t.Fatal("a landed store-first write was not recorded as unread")
	}

	msg2 := next().(persistDoneMsg)
	if msg2.err == nil {
		t.Fatal("setup: the second write was supposed to be refused")
	}
	out := m.onPersistDone(msg2)
	if out == nil {
		t.Fatal("the refusal returned no command — the landed write is now invisible " +
			"until the user reloads")
	}
	if m.rollingBack {
		t.Error("a store-first refusal opened the rollback window; nothing was applied locally")
	}
	if !strings.Contains(m.status, "epic-active-clash") {
		t.Errorf("status = %q — the refusal must name furrow's own verdict", m.status)
	}
	if strings.Contains(m.status, "rolling back") {
		t.Errorf("status = %q — a store-first refusal rolls nothing back", m.status)
	}

	// The command must be a plain re-read, not the rollback one: a rollback
	// failure says "the board may show an unsaved edit", which would be a lie.
	reload, ok := out().(reloadDoneMsg)
	if !ok {
		t.Fatalf("the refusal produced %T, want a reload", out())
	}
	if reload.rollback {
		t.Error("the re-read was marked as a rollback; nothing was optimistically applied")
	}

	// The flag is still set here: it is cleared by the re-read that APPLIES, not
	// by firing one. A reload dropped by onReloadDone's in-flight guard still
	// owes the board a re-read, and clearing early let the NEXT refusal skip it.
	if !m.unreadLanded {
		t.Error("the flag was cleared before the re-read landed")
	}
	m.onReloadDone(reload)
	if m.unreadLanded {
		t.Error("the applied re-read did not clear the flag")
	}
}

// The dropped-reconcile shape the flag's lifetime exists for: a store-first
// write lands, the drain's reconcile is DROPPED because a new write is already
// queued behind it, and that next write is refused. Without a flag that outlives
// the drain, the first write is invisible until the user presses `r`.
func TestALandedWriteSurvivesADroppedReconcile(t *testing.T) {
	m, p := storeFirstModel(t)

	goal := "着地するゴール"
	cmd := storeFirst(m, "epic set e-one", func() error {
		return p.EpicSet("e-one", board.EpicPatch{Goal: &goal})
	})
	reconcile := m.onPersistDone(cmd().(persistDoneMsg))
	if reconcile == nil {
		t.Fatal("the drained queue did not reconcile")
	}

	// A new write is queued before the reconcile lands, so onReloadDone drops it.
	p.epicErr, p.epicFailAt = errors.New("epic-not-found"), 1
	p.epicCalls = 0
	next := storeFirst(m, "epic activate e-two", func() error {
		return p.EpicActivate("e-two", "")
	})
	m.onReloadDone(reconcile().(reloadDoneMsg))
	if !m.unreadLanded {
		t.Fatal("the dropped reconcile cleared the flag it never honoured")
	}

	// That write is refused. Nothing was applied locally, so no rollback — but
	// the first write is still unread, so a re-read must be issued anyway.
	out := m.onPersistDone(next().(persistDoneMsg))
	if out == nil {
		t.Fatal("the refusal skipped the re-read the dropped reconcile still owed")
	}
	if _, ok := out().(reloadDoneMsg); !ok {
		t.Errorf("the refusal produced %T, want a reload", out())
	}
}

// The same shape on the QUICK ADD path, which had it first: an add that lands
// followed by one that is refused must still deliver the created card.
func TestARefusedAddStillReReadsTheOneThatLanded(t *testing.T) {
	m, p := storeFirstModel(t)
	// Only the SECOND call fails. Flipping addErr between the two enqueues
	// would refuse both: the Cmd runs long after the enqueue.
	p.addErr, p.addFailAt = errors.New("furrow add: exit 2"), 2

	cmd1 := m.enqueueAdd("先に通る起票", "先に通る起票", board.AddOptions{}, board.AddOptions{})
	cmd2 := m.enqueueAdd("拒否される起票", "拒否される起票", board.AddOptions{}, board.AddOptions{})
	if cmd1 == nil || cmd2 != nil {
		t.Fatalf("queue setup: cmd1=%v cmd2=%v", cmd1 != nil, cmd2 != nil)
	}

	next := m.onPersistDone(cmd1().(persistDoneMsg))
	if next == nil {
		t.Fatal("draining must fire the queued add")
	}
	if m.selectAfterReload == "" {
		t.Fatal("the landed add parked no cursor jump")
	}
	out := m.onPersistDone(next().(persistDoneMsg))
	if out == nil {
		t.Fatal("the refused add returned no command — the created card never arrives")
	}
	// The parked jump must SURVIVE, because a re-read is now on its way. It used
	// to be cleared here on the reasoning that no re-read was coming.
	if m.selectAfterReload == "" {
		t.Error("the pending cursor jump was discarded even though a re-read was queued")
	}
}

// With nothing landed there is nothing to re-read, and the parked jump must be
// dropped — otherwise it fires at some unrelated future reload.
func TestARefusedAddAloneNeitherReReadsNorKeepsAParkedJump(t *testing.T) {
	m, p := storeFirstModel(t)
	p.addErr = errors.New("furrow add: exit 2")

	cmd := m.enqueueAdd("拒否される起票", "拒否される起票", board.AddOptions{}, board.AddOptions{})
	if cmd == nil {
		t.Fatal("the add did not fire")
	}
	out := m.onPersistDone(cmd().(persistDoneMsg))
	if m.rollingBack {
		t.Error("a lone refused add opened the rollback window")
	}
	if m.selectAfterReload != "" {
		t.Errorf("a parked jump survived with no re-read coming: %q", m.selectAfterReload)
	}
	// Only the modal-reopen command may come back here, never a reload.
	if out != nil {
		if _, isReload := out().(reloadDoneMsg); isReload {
			t.Error("a lone refused add triggered a re-read; nothing landed")
		}
	}
}

// A MIXED chain still rolls back: the optimistic half is real, so the board is
// showing something the store refused.
func TestAMixedChainStillRollsBack(t *testing.T) {
	m, p := storeFirstModel(t)
	p.moveErr = errors.New("furrow set: exit 2")

	moved, cmd, err := m.commitMove("t-a", "ready", "backlog", 0)
	if err != nil || !moved || cmd == nil {
		t.Fatalf("setup: moved=%v err=%v", moved, err)
	}
	out := m.onPersistDone(cmd().(persistDoneMsg))
	if !m.rollingBack {
		t.Error("a refused optimistic write did not open the rollback window")
	}
	if out == nil {
		t.Fatal("a refused optimistic write must re-read")
	}
	got, ok := out().(reloadDoneMsg)
	if !ok {
		t.Fatalf("got %T, want a reload", out())
	}
	if !got.rollback {
		t.Error("the re-read was not marked as the rollback")
	}
}

// A failed ROLLBACK re-read closes only the rollback window. The unread window
// stays: a store-first write that LANDED is still unread, and the failed
// re-read did not change that — so the epic overlay keeps refusing until a
// re-read applies, and `r` from the board (the overlay does not route it) is
// what reopens it. Clearing the unread flags there would let a gesture aim at
// rows furrow has already moved.
//
// bite-exempt: deliberately pins behaviour main already has — the branch was
// judged intent, not a hole (t-zk3y); clearing the window there fails this test.
func TestFailedRollbackReReadKeepsTheUnreadWindow(t *testing.T) {
	m, p := storeFirstModel(t)
	sliceOnEpicAxis(t, m, "e-one")
	press(m, "m")

	// 1. A store-first write lands and is not yet re-read.
	goal := "着地したゴール"
	cmd := m.epicPatch("goal", board.EpicPatch{Goal: &goal})
	if cmd == nil {
		t.Fatal("the store-first write did not queue")
	}
	m.onPersistDone(cmd().(persistDoneMsg))
	if !m.storeFirstUnread || !m.unreadLanded {
		t.Fatalf("setup: landed write not marked unread (storeFirst=%v landed=%v)",
			m.storeFirstUnread, m.unreadLanded)
	}

	// 2. An optimistic write is refused by the store, arming the rollback.
	fail := m.enqueuePersist("move t-a", func() ([]string, error) {
		return nil, errors.New("schema gate says no")
	})
	if fail == nil {
		t.Fatal("setup: the optimistic write did not queue")
	}
	rb := m.onPersistDone(fail().(persistDoneMsg))
	if rb == nil || !m.rollingBack {
		t.Fatal("setup: the refused optimistic write must arm the rollback window")
	}

	// 3. The rollback re-read itself fails.
	m.onReloadDone(reloadDoneMsg{rollback: true, err: errors.New("git lock")})
	if m.rollingBack {
		t.Fatal("a failed rollback re-read must close the rollback window")
	}
	if !m.unreadLanded || !m.storeFirstUnread {
		t.Fatalf("the failed re-read cleared the unread window (landed=%v storeFirst=%v) — "+
			"the landed write is still unread", m.unreadLanded, m.storeFirstUnread)
	}

	// 4. An epic gesture is refused, and the reason names the missing re-read.
	before := len(p.calls)
	m.status, m.statusErr = "", false
	if c := m.epicPatch("goal", board.EpicPatch{Goal: &goal}); c != nil {
		t.Error("an epic gesture queued against rows the board has not re-read")
	}
	if len(p.calls) != before {
		t.Errorf("the store saw another call: %v", p.calls[before:])
	}
	if !m.statusErr || !strings.Contains(m.status, "landed; waiting for the board to re-read it") {
		t.Errorf("status = %q — the refusal must name the LANDED write, not an in-flight one", m.status)
	}
	if !strings.Contains(m.status, "esc out, then r") {
		t.Errorf("status = %q — the refusal must name the way out; r is not routed in the overlay", m.status)
	}

	// 5. `r` inside the overlay is not routed (it types nothing and fires
	// nothing); esc out to the board and `r` fires the re-read that applies
	// and reopens the overlay.
	press(m, "r")
	if m.mode != modeEpic || !m.storeFirstUnread {
		t.Fatalf("r inside the overlay: mode = %v unread = %v — it must neither leave nor re-read",
			m.mode, m.storeFirstUnread)
	}
	press(m, "esc", "esc")
	if m.mode != modeNormal {
		t.Fatalf("mode = %v after esc esc, want normal", m.mode)
	}
	_, c := m.Update(keyMsg("r"))
	if c == nil {
		t.Fatal("r on the board did not fire a reload")
	}
	reload, ok := c().(reloadDoneMsg)
	if !ok {
		t.Fatalf("r produced %T, want a reload", c())
	}
	m.onReloadDone(reload)
	if m.unreadLanded || m.storeFirstUnread {
		t.Error("the applied re-read did not clear the unread window")
	}
	sliceOnEpicAxis(t, m, "e-one")
	press(m, "m")
	if c := m.epicPatch("goal", board.EpicPatch{Goal: &goal}); c == nil {
		t.Error("the overlay stayed locked after the re-read landed")
	}
}

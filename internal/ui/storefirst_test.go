package ui

import (
	"errors"
	"strings"
	"testing"

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
	cmd1 := m.enqueueStoreFirst("epic set e-one", func() error {
		return p.EpicSet("e-one", board.EpicPatch{Goal: &goal})
	})
	cmd2 := m.enqueueStoreFirst("epic activate e-two", func() error {
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
	cmd := m.enqueueStoreFirst("epic set e-one", func() error {
		return p.EpicSet("e-one", board.EpicPatch{Goal: &goal})
	})
	reconcile := m.onPersistDone(cmd().(persistDoneMsg))
	if reconcile == nil {
		t.Fatal("the drained queue did not reconcile")
	}

	// A new write is queued before the reconcile lands, so onReloadDone drops it.
	p.epicErr, p.epicFailAt = errors.New("epic-not-found"), 1
	p.epicCalls = 0
	next := m.enqueueStoreFirst("epic activate e-two", func() error {
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

	cmd1 := m.enqueueAdd("先に通る起票", board.AddOptions{})
	cmd2 := m.enqueueAdd("拒否される起票", board.AddOptions{})
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

	cmd := m.enqueueAdd("拒否される起票", board.AddOptions{})
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

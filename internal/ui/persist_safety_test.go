package ui

import (
	"errors"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The rollback window and the refused-add contract (t-74y3). Between a
// failed write and its rollback re-read the board shows state the store
// refused: gestures there both preempt the re-read and can address the
// wrong store row. And a store refusing an add must hand the typed title
// back, never eat it — without stealing a mode the user has moved on to.

// Between a failed write and its rollback re-read the board shows state the
// store refused: gestures there must be refused — queueing them both
// preempts the rollback and lets index-addressed writes hit the wrong row.
func TestFailedWriteRefusesGesturesUntilTheRollbackLands(t *testing.T) {
	m, p := scriptedModel(t)
	p.moveErr = errors.New("schema gate says no")

	_, cmd, err := m.commitMove("a", "ready", "ready", 0, 3)
	if err != nil || cmd == nil {
		t.Fatal(err)
	}
	rb := m.onPersistDone(cmd().(persistDoneMsg))
	if rb == nil {
		t.Fatal("a failed persist must schedule the rollback re-read")
	}

	p.moveErr = nil
	moved, cmd2, _ := m.commitMove("c", "ready", "ready", 1, 0)
	if cmd2 != nil || len(m.pending) != 0 {
		t.Fatalf("a gesture inside the rollback window must be refused (moved=%v pending=%d)",
			moved, len(m.pending))
	}

	m.Update(rb())
	if got := laneIDs(m.b, "ready"); got != "a,b,c" {
		t.Fatalf("rollback re-read must land: ready = %s, want a,b,c", got)
	}

	_, cmd3, err := m.commitMove("c", "ready", "ready", 2, 0)
	if err != nil || cmd3 == nil {
		t.Fatal("once the rollback lands, gestures must flow again")
	}
}

// A failed rollback re-read must CLOSE the window, not gate every write
// forever behind a re-read that may never succeed — the loud status line
// hands the user the retry instead.
func TestFailedRollbackReReadClosesTheWindow(t *testing.T) {
	m, p := scriptedModel(t)
	p.moveErr = errors.New("schema gate says no")

	_, cmd, _ := m.commitMove("a", "ready", "ready", 0, 3)
	m.onPersistDone(cmd().(persistDoneMsg)) // rollingBack armed
	if !m.rollingBack {
		t.Fatal("setup: the failed persist must arm the window")
	}

	m.onReloadDone(reloadDoneMsg{rollback: true, err: errors.New("git lock")})
	if m.rollingBack {
		t.Fatal("a failed rollback re-read must close the window")
	}
	if !m.statusErr {
		t.Error("the failed re-read must surface as an error")
	}

	p.moveErr = nil
	if _, cmd2, err := m.commitMove("c", "ready", "ready", 1, 0); err != nil || cmd2 == nil {
		t.Fatal("after the window closes, gestures must flow again")
	}
}

// An add committed inside the rollback window keeps its modal (and the
// typed title) instead of queueing a write against a lying board.
func TestAddInsideTheRollbackWindowKeepsItsModal(t *testing.T) {
	m, p := scriptedModel(t)
	p.moveErr = errors.New("schema gate says no")

	_, cmd, _ := m.commitMove("a", "ready", "ready", 0, 3)
	m.onPersistDone(cmd().(persistDoneMsg)) // rollingBack armed

	press(m, "a")
	m.add.input.SetValue("keep me")
	if _, c := m.Update(keyMsg("enter")); c != nil {
		t.Fatal("an add inside the window must not fire")
	}
	if m.mode != modeAdd || m.add == nil || m.add.input.Value() != "keep me" {
		t.Fatal("the modal and its title must survive the refusal")
	}
	if len(m.pending) != 0 {
		t.Fatal("nothing may queue inside the window")
	}
}

// A store refusal must hand the typed title back, not eat it.
func TestRefusedAddReopensWithTheTitle(t *testing.T) {
	m, p := scriptedModel(t)
	p.addErr = errors.New("epic e-nope not found")

	press(m, "a")
	m.add.input.SetValue("keep me")
	_, addCmd := m.Update(keyMsg("enter"))
	if addCmd == nil {
		t.Fatal("add did not fire")
	}
	m.Update(addCmd())

	if m.mode != modeAdd || m.add == nil {
		t.Fatal("a refusal must reopen the modal")
	}
	if got := m.add.input.Value(); got != "keep me" {
		t.Fatalf("reopened title = %q, want the typed text back", got)
	}
	if !m.statusErr {
		t.Error("the refusal must surface as an error")
	}
}

// A refusal landing after the user moved on must not steal the newer mode —
// reopening the modal ~100ms later would clobber a second half-typed draft.
func TestRefusalDoesNotStealANewerMode(t *testing.T) {
	m, p := scriptedModel(t)
	p.addErr = errors.New("epic e-nope not found")

	press(m, "a")
	m.add.input.SetValue("first")
	_, firstCmd := m.Update(keyMsg("enter"))
	if firstCmd == nil {
		t.Fatal("first add did not fire")
	}

	press(m, "a") // the user has moved on to a SECOND draft
	m.add.input.SetValue("second draft")

	m.Update(firstCmd()) // the first add's refusal lands now
	if m.mode != modeAdd || m.add == nil {
		t.Fatal("the newer modal must survive the older refusal")
	}
	if got := m.add.input.Value(); got != "second draft" {
		t.Fatalf("the newer draft was clobbered: %q", got)
	}
	if !m.statusErr {
		t.Error("the refusal must still surface as an error")
	}
}

// A reload that does not yet contain the just-created card must not consume
// the pending selection — the create's own snapshot is still on its way.
func TestSelectAfterReloadWaitsForItsCard(t *testing.T) {
	m, p := scriptedModel(t)
	m.selectAfterReload = "n1"

	m.onReloadDone(reloadDoneMsg{}) // a foreign reload, n1 not in the store yet
	if m.selectAfterReload != "n1" {
		t.Fatal("a reload without the card must not consume the pending selection")
	}

	p.mu.Lock()
	p.current = board.NewBoard([]*board.Task{
		{ID: "a", Title: "a", Status: "ready", Priority: 10},
		{ID: "n1", Title: "the new card", Status: "backlog", Priority: 5},
	})
	p.mu.Unlock()
	m.onReloadDone(reloadDoneMsg{})
	if m.selectAfterReload != "" {
		t.Fatal("the reload that delivers the card must land the selection")
	}
	if cur := m.curTask(); cur == nil || cur.ID != "n1" {
		t.Fatalf("cursor must sit on the new card, got %v", cur)
	}
}

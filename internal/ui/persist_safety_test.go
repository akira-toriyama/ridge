package ui

import (
	"errors"
	"strings"
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

	_, cmd, err := m.commitMove("a", "ready", "ready", 3)
	if err != nil || cmd == nil {
		t.Fatal(err)
	}
	rb := m.onPersistDone(cmd().(persistDoneMsg))
	if rb == nil {
		t.Fatal("a failed persist must schedule the rollback re-read")
	}

	p.moveErr = nil
	moved, cmd2, _ := m.commitMove("c", "ready", "ready", 0)
	if cmd2 != nil || len(m.pending) != 0 {
		t.Fatalf("a gesture inside the rollback window must be refused (moved=%v pending=%d)",
			moved, len(m.pending))
	}

	m.Update(rb())
	if got := laneIDs(m.b, "ready"); got != "a,b,c" {
		t.Fatalf("rollback re-read must land: ready = %s, want a,b,c", got)
	}

	_, cmd3, err := m.commitMove("c", "ready", "ready", 0)
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

	_, cmd, _ := m.commitMove("a", "ready", "ready", 3)
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
	if _, cmd2, err := m.commitMove("c", "ready", "ready", 0); err != nil || cmd2 == nil {
		t.Fatal("after the window closes, gestures must flow again")
	}
}

// An add committed inside the rollback window keeps its modal (and the
// typed title) instead of queueing a write against a lying board.
func TestAddInsideTheRollbackWindowKeepsItsModal(t *testing.T) {
	m, p := scriptedModel(t)
	p.moveErr = errors.New("schema gate says no")

	_, cmd, _ := m.commitMove("a", "ready", "ready", 3)
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

// A gesture refused by the window must LEAVE the refusal on the status line:
// the callers' own success notes run after commitMove returns, and papering
// over the refusal tells the user the move worked right before the rollback
// yanks it back (review F14-1). The gesture is refused before the optimistic
// apply, so the board must not change either.
func TestRefusedGestureKeepsTheRefusalOnTheStatusLine(t *testing.T) {
	m, p := scriptedModel(t)
	p.moveErr = errors.New("schema gate says no")

	_, cmd, _ := m.commitMove("a", "ready", "ready", 3)
	m.onPersistDone(cmd().(persistDoneMsg)) // rollingBack armed
	before := laneIDs(m.b, "ready")

	m.selectID("a", false) // sits at the bottom after the refused move's optimistic apply
	if c := m.quickReorder(-1); c != nil {
		t.Fatal("a reorder inside the window must not fire")
	}
	if !m.statusErr || !strings.Contains(m.status, "dropped") {
		t.Fatalf("status = %q — the refusal must survive the gesture's own note", m.status)
	}
	if got := laneIDs(m.b, "ready"); got != before {
		t.Fatalf("a refused gesture must not touch the board: %s -> %s", before, got)
	}
}

// A refusal landing while the user is in the graph view must not reopen the
// modal: the graph shares modeNormal but never composites addLayer, so the
// reopen would put the keyboard inside an invisible input (review F14-3).
func TestRefusalDoesNotReopenInTheGraphView(t *testing.T) {
	m, p := scriptedModel(t)
	p.addErr = errors.New("epic e-nope not found")

	press(m, "a")
	m.add.input.SetValue("unseen")
	_, addCmd := m.Update(keyMsg("enter"))
	if addCmd == nil {
		t.Fatal("add did not fire")
	}
	m.view = viewGraph // the user switched to the graph before the refusal lands
	m.Update(addCmd())

	if m.mode != modeNormal || m.add != nil {
		t.Fatal("the refusal must not steal the keyboard into an invisible modal")
	}
	if !m.statusErr {
		t.Error("the refusal must still surface as an error")
	}
}

// …and the help overlay is the OTHER layer that hides addLayer (zHelp sits
// above it): a refusal landing under an open `?` must not reopen either, or
// the keyboard ends up inside an invisible modal that `?` types into instead
// of closing (independent review of t-8xk8, F1 — the last ungated mode
// assignment in the package).
func TestRefusalDoesNotReopenUnderTheHelpOverlay(t *testing.T) {
	m, p := scriptedModel(t)
	p.addErr = errors.New("epic e-nope not found")

	press(m, "a")
	m.add.input.SetValue("unseen")
	_, addCmd := m.Update(keyMsg("enter"))
	if addCmd == nil {
		t.Fatal("add did not fire")
	}
	press(m, "?") // the user opened the help before the refusal lands
	m.Update(addCmd())

	if m.mode != modeNormal || m.add != nil {
		t.Fatal("the refusal must not steal the keyboard into a modal the overlay hides")
	}
	if !m.statusErr {
		t.Error("the refusal must still surface as an error")
	}
}

// A $EDITOR body landing inside the rollback window is the one write whose
// payload cannot be re-typed from a note (the temp file is already deleted):
// it must be HELD and replayed once the window closes, not refused
// (review F14-2).
func TestEditorBodyIsHeldThroughTheRollbackWindow(t *testing.T) {
	m, p := scriptedModel(t)
	p.moveErr = errors.New("schema gate says no")

	_, cmd, _ := m.commitMove("a", "ready", "ready", 3)
	rb := m.onPersistDone(cmd().(persistDoneMsg)) // rollingBack armed

	m.Update(editorDoneMsg{id: "b", body: "hand-typed prose"})
	if m.heldBody == nil {
		t.Fatal("the body must be held, not refused")
	}
	if got := m.b.Task("b").Body; got == "hand-typed prose" {
		t.Fatal("the held body must not apply while the board shows refused state")
	}
	if q := m.quitOrFlush(); q != nil {
		t.Fatal("quit must wait for the held body")
	}

	m.Update(rb()) // the rollback re-read lands: the window closes
	if m.heldBody != nil {
		t.Fatal("the hold must be consumed when the window closes")
	}
	if got := m.b.Task("b").Body; got != "hand-typed prose" {
		t.Fatalf("the replay must apply the body, got %q", got)
	}
	if len(m.pending) == 0 && !m.inflight {
		t.Fatal("the replay must queue the store write")
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

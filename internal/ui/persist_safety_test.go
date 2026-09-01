package ui

import (
	"errors"
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"

	tea "charm.land/bubbletea/v2"
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
// yanks it back. The gesture is refused before the optimistic apply, so the
// board must not change either.
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

// The gestures below are one contract (t-74y3, t-6fvd): while the window is
// open, nothing the rollback cannot take back may happen — no m.b mutation, and
// no modal closing over typed text. refuseWhileRollingBack's doc carries the
// why; what matters here is that removing a guard reddens only the board and
// text assertions, never the status ones (measured), which is why each test
// keeps both.

// `d` reaches enqueuePersist only after Board.Close has already moved the card
// into the done lane.
func TestDoneIsRefusedWhileRollingBack(t *testing.T) {
	m, p := scriptedModel(t)
	p.moveErr = errors.New("schema gate says no")

	_, cmd, _ := m.commitMove("a", "ready", "ready", 3)
	m.onPersistDone(cmd().(persistDoneMsg)) // rollingBack armed
	if !m.rollingBack {
		t.Fatal("a refused write must arm the rollback window")
	}

	if !m.selectID("b", false) {
		t.Fatal("b must be selectable")
	}
	before, writes := laneIDs(m.b, "ready"), len(p.calls)

	if _, c := m.Update(keyMsg("d")); c != nil {
		t.Error("done must not enqueue a write inside the rollback window")
	}
	if got := laneIDs(m.b, "ready"); got != before {
		t.Errorf("a refused gesture must not touch the board: %s -> %s", before, got)
	}
	if got := m.b.Task("b").Status; got == m.b.DoneLane() {
		t.Errorf("b was closed anyway (status %q) — the refusal came too late", got)
	}
	if !m.statusErr || !strings.Contains(m.status, "dropped") {
		t.Errorf("status = %q, want the refusal — a success note must not overwrite it", m.status)
	}
	if got := p.calls[writes:]; len(got) != 0 {
		t.Errorf("the store was written to inside the window: %v", got)
	}
}

// applyPatch is the funnel for every FIELD gesture in the edit overlay — title,
// due, value, effort, epic, and the label/repo/ref rows — so the guard there
// covers all of them; the retitle path stands in for the set.
//
// A commit from an INPUT is refused one step earlier still, before the Commit
// branch blurs and drops the stage, because hand-typed text the modal has
// closed over cannot be recovered. inputNote has always been refused that
// early; the rest are now too, which is the second half of this test.
func TestFieldEditIsRefusedWhileRollingBack(t *testing.T) {
	m := editModel(t, "t-9sa6")
	before := m.b.Task("t-9sa6").Title

	press(m, "enter") // menu row 0 = title, opens the input
	m.edit.input.SetValue("巻き戻し中の幽霊タイトル")
	m.rollingBack = true
	press(m, "enter")

	if got := m.b.Task("t-9sa6").Title; got != before {
		t.Errorf("the retitle landed on a rolling-back board: %q", got)
	}
	if m.inflight || len(m.pending) > 0 {
		t.Error("nothing must reach the persist queue in the rollback window")
	}
	if !m.statusErr {
		t.Error("the refusal must land on the status row")
	}
	if m.edit == nil || m.edit.stage != stageInput {
		t.Fatal("the input must stay open — a closed one loses the typed text")
	}
	if got := m.edit.input.Value(); strings.TrimSpace(got) != "巻き戻し中の幽霊タイトル" {
		t.Errorf("the typed text was eaten by the refusal: %q", got)
	}
}

// The pick stage reaches applyPatch with no input in the way, so it is what
// covers that funnel's own guard — the retitle above is stopped one layer
// earlier, at the input commit, and so cannot.
func TestValuePickIsRefusedWhileRollingBack(t *testing.T) {
	m := editModel(t, "t-9sa6")
	before := m.b.Task("t-9sa6").Value

	m.edit.menuIdx = int(fieldValue)
	press(m, "enter") // into the value pick
	if m.edit.stage != stagePick {
		t.Fatalf("the value pick did not open: stage=%d", m.edit.stage)
	}
	m.rollingBack = true
	press(m, "3")

	if got := m.b.Task("t-9sa6").Value; got != before {
		t.Errorf("the pick landed on a rolling-back board: value %d -> %d", before, got)
	}
	if m.inflight || len(m.pending) > 0 {
		t.Error("nothing must reach the persist queue in the rollback window")
	}
	if !m.statusErr {
		t.Error("the refusal must land on the status row")
	}
}

// applyCheck is applyPatch's twin for the checklist and dep ops. Same funnel
// argument: dep rm stands in for check add/rm/reword/toggle and dep add.
func TestChecklistAndDepEditsAreRefusedWhileRollingBack(t *testing.T) {
	m := editModel(t, "t-jv3j") // waits on t-ehk7 (open) and t-t38k (done)
	before := strings.Join(m.b.Task("t-jv3j").Deps, ",")

	m.edit.menuIdx = int(fieldDeps)
	press(m, "enter") // into the deps list
	m.rollingBack = true
	press(m, "enter") // ⏎ on a row removes the edge

	if got := strings.Join(m.b.Task("t-jv3j").Deps, ","); got != before {
		t.Errorf("the edge was cut on a rolling-back board: %q -> %q", before, got)
	}
	if m.inflight || len(m.pending) > 0 {
		t.Error("nothing must reach the persist queue in the rollback window")
	}
	if !m.statusErr {
		t.Error("the refusal must land on the status row")
	}
}

// A refusal landing while the user is in the graph view must not reopen the
// modal: the graph shares modeNormal but never composites addLayer, so the
// reopen would put the keyboard inside an invisible input.
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
// of closing — this was the last ungated mode assignment in the package
// (t-8xk8).
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
// it must be HELD and replayed once the window closes, not refused.
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

// A held body whose replay is REFUSED (a wiped $EDITOR buffer: SetBody
// mirrors furrow's empty-replacement refusal) is removed from the drain
// without queueing anything — so a quit armed on it must be cancelled, or
// nothing is left to fire tea.Quit and the armed flag turns the next
// unrelated write into a surprise exit (found by the PR #66 refute review).
func TestRefusedHeldBodyCancelsTheQuitItArmed(t *testing.T) {
	m, p := scriptedModel(t)
	p.moveErr = errors.New("schema gate says no")

	_, cmd, _ := m.commitMove("a", "ready", "ready", 3)
	rb := m.onPersistDone(cmd().(persistDoneMsg)) // rollingBack armed

	m.Update(editorDoneMsg{id: "b", body: " \n"}) // a wiped $EDITOR buffer
	if m.heldBody == nil {
		t.Fatal("the body must be held through the window — it is judged at replay, not here")
	}
	if q := m.quitOrFlush(); q != nil {
		t.Fatal("quit must wait for the held body")
	}

	m.Update(rb()) // the window closes; the replay is refused
	if m.heldBody != nil {
		t.Fatal("the hold must be consumed even when the replay is refused")
	}
	if !m.statusErr {
		t.Error("the refusal must surface as an error")
	}
	// That the refusal keeps the old body is Board.SetBody's contract, proved
	// in board's TestSetBodyRefusesWhatFurrowWould — the fixture reload here
	// rebuilds the board, so asserting it again on m.b would be vacuous.
	if len(m.pending) != 0 || m.inflight {
		t.Fatal("a refused replay must queue nothing")
	}
	if m.quitting {
		t.Fatal("the refused replay must cancel the quit it armed — nothing is left to fire tea.Quit")
	}

	// The second-order failure the strand caused: a later unrelated write
	// draining an empty queue must reconcile, not fire a stale tea.Quit.
	p.mu.Lock()
	p.moveErr = nil
	p.mu.Unlock()
	_, cmd2, _ := m.commitMove("a", "ready", "ready", 3)
	if done := m.onPersistDone(cmd2().(persistDoneMsg)); done != nil {
		if _, quit := done().(tea.QuitMsg); quit {
			t.Fatal("a later write fired the quit the refused replay left armed")
		}
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

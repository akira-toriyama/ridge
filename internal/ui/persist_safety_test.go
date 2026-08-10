package ui

import (
	"errors"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The single-writer discipline and the rollback window (t-74y3). Four
// verified defects lived here: the checklist-toggle closure read the live
// board off-thread (a data race, and an index-out-of-range that shut the
// program down with every queued edit unpersisted); quick add bypassed both
// the persist queue (two concurrent furrow writers — measured 15/20 lost
// writes) and the quit guard; a refused add ate the typed title; and any
// gesture inside a failed write's rollback window both preempted the
// rollback re-read and could address the wrong store row.

// checklistModel is a scripted model whose one task carries a 2-item
// checklist — the smallest board on which "toggle then delete" can corrupt.
func checklistModel(t *testing.T) (*Model, *scriptedProvider) {
	t.Helper()
	p := newScriptedProvider(func() *board.Board {
		return board.NewBoard([]*board.Task{
			{ID: "a", Title: "a", Status: "ready", Priority: 10,
				Checklist: []board.ChecklistItem{{Text: "A"}, {Text: "B"}}},
		})
	})
	m := New(p, Options{})
	m.w, m.h = 140, 40
	m.recompute()
	m.relayout()
	return m, p
}

// Toggling an item and deleting its neighbour before the queue drains must
// persist the value the toggle SAW — not read the mutated board from the
// write goroutine (which panicked once the index vanished).
func TestChecklistTogglePersistsTheValueItSaw(t *testing.T) {
	m, p := checklistModel(t)
	if !m.selectID("a", false) {
		t.Fatal("could not select a")
	}
	m.enterEdit()
	m.edit.menuIdx = int(fieldChecklist)
	press(m, "enter", "down") // checklist stage, cursor on item 1 (B)
	press(m, "x")             // toggle B — persist op queued
	press(m, "up")
	press(m, "d") // delete item 0 (A) — queued behind the toggle

	// Pre-fix the toggle closure indexes the mutated live board at drain
	// time and panics; convert that to a clean failure so the go-bite trial
	// of the OTHER tests still gets to run against the pre-change tree.
	panicked := func() (panicked bool) {
		defer func() {
			if recover() != nil {
				panicked = true
			}
		}()
		drainPersists(m, t)
		return false
	}()
	if panicked {
		t.Fatal("the toggle closure read the live board at drain time and blew up")
	}

	if len(p.checkVals) != 1 || p.checkVals[0] != "check a 1=true" {
		t.Errorf("persisted toggle = %v, want [check a 1=true] — the value the gesture saw", p.checkVals)
	}
}

// An add committed while a write is in flight must wait for the drain: the
// store has exactly one writer at a time.
func TestAddWaitsForTheQueueToDrain(t *testing.T) {
	m, p := scriptedModel(t)

	_, cmd1, err := m.commitMove("a", "ready", "ready", 0, 3)
	if err != nil || cmd1 == nil {
		t.Fatalf("move: cmd=%v err=%v", cmd1, err)
	}

	press(m, "a")
	if m.add == nil {
		t.Fatal("quick-add modal did not open")
	}
	m.add.input.SetValue("while busy")
	_, addCmd := m.Update(keyMsg("enter"))
	if addCmd != nil {
		t.Fatal("an add committed mid-flight must defer to the drain, not fire its own writer")
	}
	if len(p.addCalls) != 0 {
		t.Fatalf("store saw Add %v while a move was in flight", p.addCalls)
	}

	next := m.onPersistDone(cmd1().(persistDoneMsg))
	if next == nil {
		t.Fatal("the drain must fire the deferred add")
	}
	msg, ok := next().(addDoneMsg)
	if !ok {
		t.Fatalf("drain fired %T, want the add", msg)
	}
	if len(p.addCalls) != 1 || p.addCalls[0] != "add while busy" {
		t.Fatalf("add calls = %v", p.addCalls)
	}
}

// `q` with an add in flight must wait for its outcome — quitting immediately
// makes a refusal (or the created id) silently unobservable.
func TestQuitWaitsForAnInFlightAdd(t *testing.T) {
	m, _ := scriptedModel(t)

	press(m, "a")
	m.add.input.SetValue("almost lost")
	_, addCmd := m.Update(keyMsg("enter"))
	if addCmd == nil {
		t.Fatal("an add on an idle queue must fire")
	}

	if q := m.quitOrFlush(); q != nil {
		t.Fatal("quit must wait for the in-flight add, not leave on top of it")
	}
	if !m.quitting {
		t.Fatal("the deferred quit must be remembered")
	}

	_, after := m.Update(addCmd())
	if after == nil {
		t.Fatal("landing the add while quitting must resume the quit")
	}
	if _, ok := after().(tea.QuitMsg); !ok {
		t.Fatalf("after the flush the resumed cmd must quit, got %T", after())
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

	m.onReloadDone(rb().(reloadDoneMsg))
	if got := laneIDs(m.b, "ready"); got != "a,b,c" {
		t.Fatalf("rollback re-read must land: ready = %s, want a,b,c", got)
	}

	_, cmd3, err := m.commitMove("c", "ready", "ready", 2, 0)
	if err != nil || cmd3 == nil {
		t.Fatal("once the rollback lands, gestures must flow again")
	}
}

// A failed rollback re-read must still drain what waited on the window: a
// deferred add stranded here had no remaining path to fire, and quitOrFlush
// counts deferred adds — q and ctrl+c were wedged with no keyboard way out.
func TestFailedRollbackReReadStillDrainsADeferredAdd(t *testing.T) {
	m, p := scriptedModel(t)
	p.moveErr = errors.New("schema gate says no")

	_, cmd, _ := m.commitMove("a", "ready", "ready", 0, 3)
	m.onPersistDone(cmd().(persistDoneMsg)) // rollingBack armed

	press(m, "a")
	m.add.input.SetValue("stranded?")
	if _, c := m.Update(keyMsg("enter")); c != nil {
		t.Fatal("the add must defer during the rollback window")
	}

	next := m.onReloadDone(reloadDoneMsg{err: errors.New("git lock")})
	if next == nil {
		t.Fatal("a failed rollback re-read must still drain the deferred add")
	}
	if _, ok := next().(addDoneMsg); !ok {
		t.Fatal("the drained cmd must be the add")
	}
	if len(p.addCalls) != 1 {
		t.Fatalf("add calls = %v", p.addCalls)
	}
	if q := m.quitOrFlush(); q == nil && !m.addInFlight && len(m.deferredAdds) == 0 {
		t.Fatal("quit is wedged with nothing left to wait for")
	}
}

// A refusal landing after the user moved on must not steal the newer mode —
// reopening the modal ~100ms later clobbered a second half-typed draft.
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

package ui

import (
	"testing"
)

// The mid-move half of the t-74y3 filter-lifecycle findings: a verdict landing
// mid-keyboard-move reshaped the columns under the aimed slot, so the commit
// wrote a different position than the status line promised. (The other half —
// the startup -filter arming nothing on a live provider — is covered by
// TestStartupFilterReachesTheLiveStore in hardening_test.go.)

func TestMoveHoldsTheVerdictUntilTheMoveResolves(t *testing.T) {
	m, _ := scriptedModel(t)

	m.applyFilter("title:x")
	m.Update(filterResultMsg{seq: m.qSeq, ids: []string{"a", "b", "c", "z"}})
	if got := len(m.cols["ready"]); got != 3 {
		t.Fatalf("setup: ready shows %d, want 3", got)
	}

	press(m, "enter") // lift the cursor's card — keyboard move mode
	if m.mode != modeMove {
		t.Fatalf("enter must lift the card, mode=%v", m.mode)
	}

	// A shrinking verdict lands mid-aim: it must NOT reshape the columns the
	// drop slot is measured against.
	m.Update(filterResultMsg{seq: m.qSeq, ids: []string{"a"}})
	if got := len(m.cols["ready"]); got != 3 {
		t.Fatalf("mid-move verdict reshaped the board: ready shows %d, want 3 held", got)
	}
	if m.heldVerdict == nil {
		t.Fatal("the mid-move verdict must be held for the exit")
	}

	press(m, "esc") // cancel the move — the held verdict applies now
	if got := len(m.cols["ready"]); got != 1 {
		t.Fatalf("after the move exits the held verdict must land: ready shows %d, want 1", got)
	}
	if m.heldVerdict != nil {
		t.Fatal("the hold must be consumed on exit")
	}
}

func TestStaleHeldVerdictIsDroppedOnRelease(t *testing.T) {
	m, _ := scriptedModel(t)
	m.applyFilter("title:x")
	m.Update(filterResultMsg{seq: m.qSeq, ids: []string{"a", "b", "c", "z"}})

	press(m, "enter")
	m.Update(filterResultMsg{seq: m.qSeq, ids: []string{"a"}}) // held
	m.applyFilter("title:xy")                                  // newer keystroke re-queries: hold is stale
	press(m, "esc")
	if got := len(m.cols["ready"]); got != 3 {
		t.Fatalf("a stale held verdict must be dropped, ready shows %d, want 3", got)
	}
}

// A held verdict released by a commit that queued a persist must NOT land over
// the optimistic move — the queued-writes guard applies on release too, and
// the post-drain reconcile requeries instead.
func TestHeldVerdictYieldsToTheCommitsQueuedWrite(t *testing.T) {
	m, _ := scriptedModel(t)
	m.applyFilter("title:x")
	m.Update(filterResultMsg{seq: m.qSeq, ids: []string{"a", "b", "c", "z"}})

	m.selectID("a", false) // lift from the three-card lane, not backlog's only card
	press(m, "enter")
	m.Update(filterResultMsg{seq: m.qSeq, ids: []string{"a"}}) // held mid-aim
	press(m, "down", "down", "enter")                          // commit: queues the persist
	if len(m.pending) == 0 && !m.inflight {
		t.Fatal("setup: the commit must queue a persist")
	}
	if got := len(m.cols["ready"]); got != 3 {
		t.Fatalf("the held verdict landed over the queued move: ready shows %d, want 3", got)
	}
	if m.heldVerdict != nil {
		t.Fatal("the hold must be consumed on exit either way")
	}
}

package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// `d`'s note counts what the close FREES, not how many open tasks depend on
// it. On the 100-task ridge-test board, closing one of a decision's six
// blockers announced "unblocked 1 task(s)" while the decision stayed held by
// the other five (t-h9pb).
func TestDoneNoteCountsOnlyTheTasksTheCloseFrees(t *testing.T) {
	m, _ := scriptedModel(t)
	// c waits on b AND on z, both open: closing b frees nothing.
	m.b.Task("c").Deps = []string{"b", "z"}
	m.recompute()
	m.selectID("b", false)
	if cmd := m.onNormalKey(tea.KeyPressMsg{Code: 'd', Text: "d"}); cmd == nil {
		t.Fatal("done must return the persist Cmd")
	}
	if m.status != "closed b" {
		t.Fatalf("status = %q, want the bare close: c is still held by z", m.status)
	}
	// z was c's last open blocker, so its close is the one that frees c. (The
	// second write queues behind the first; the note is written on the gesture.)
	m.selectID("z", false)
	m.onNormalKey(tea.KeyPressMsg{Code: 'd', Text: "d"})
	if m.status != "closed z — unblocked 1 task(s)" {
		t.Fatalf("status = %q, want the one task z's close frees", m.status)
	}
}

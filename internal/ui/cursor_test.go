package ui

import (
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
)

// `>` (jumpToBlocker) chooses which blocker the cursor lands on. What `<`
// then pins is asserted against the frame in frametruth_test.go.

// jumpToBlocker follows Deps[0] blindly; when the first dep is DONE and a later
// one is not, it jumps to a satisfied task and calls it "blocker 1/N".
func TestAdvJumpToBlockerReportsTheWrongCount(t *testing.T) {
	b := board.NewBoard([]*board.Task{
		{ID: "d1", Title: "closed", Status: "done", Priority: 10},
		{ID: "d2", Title: "open", Status: "ready", Priority: 10},
		{ID: "me", Title: "me", Status: "ready", Priority: 20, Deps: []string{"d1", "d2"}},
	})
	m := New(&emptyProvider{b: b}, Options{})
	m.w, m.h = 140, 40
	m.recompute()
	m.relayout()
	if !m.selectID("me", false) {
		t.Fatal("cannot select me")
	}
	m.jumpToBlocker()
	if got := m.curTask(); got == nil || got.ID != "d2" {
		id := "<nil>"
		if got != nil {
			id = got.ID
		}
		t.Errorf("jumped to %s; the only real blocker is d2 (d1 is done). status=%q", id, m.status)
	}
}

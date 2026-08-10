package ui

import (
	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
	"strings"
	"testing"
)

// boardInsertIndex is the OTHER translation: a drop measured in a FILTERED
// column has to land in the right slot of the full lane.
func TestBoardInsertIndexUnderAFilter(t *testing.T) {
	full := []*board.Task{{ID: "a"}, {ID: "hidden1"}, {ID: "b"}, {ID: "hidden2"}, {ID: "c"}}
	vis := []*board.Task{{ID: "a"}, {ID: "b"}, {ID: "c"}}

	tests := []struct{ visIdx, want int }{
		{0, 0}, // before a
		{1, 2}, // before b, which is really index 2
		{2, 4}, // before c, which is really index 4
		{3, 5}, // past the end of the visible list => end of the full lane
		{9, 5},
	}
	for _, tc := range tests {
		if got := boardInsertIndex(full, vis, tc.visIdx); got != tc.want {
			t.Errorf("boardInsertIndex(visIdx=%d) = %d, want %d", tc.visIdx, got, tc.want)
		}
	}

	// With no filter the translation is the identity.
	for i := 0; i <= len(vis); i++ {
		if got := boardInsertIndex(vis, vis, i); got != i {
			t.Errorf("unfiltered translation must be identity: %d -> %d", i, got)
		}
	}
}

// The filtered-drop end to end: the board must reorder around hidden tasks
// rather than counting them as slots.
func TestCommitMoveRespectsHiddenTasks(t *testing.T) {
	m := New(memstore.New(), Options{})
	m.w, m.h = 140, 40
	m.applyFilter("label:ui")
	m.recompute()

	vis := m.cols["backlog"]
	if len(vis) < 3 {
		t.Fatalf("need at least 3 visible backlog tasks, got %d", len(vis))
	}
	full := m.b.LaneTasks("backlog")
	if len(full) == len(vis) {
		t.Fatal("this test needs the filter to actually hide something")
	}
	mover, anchor := vis[0].ID, vis[2].ID

	// Drop the first visible card just before the third visible card.
	if _, _, err := m.commitMove(mover, "backlog", "backlog", 0, 2); err != nil {
		t.Fatal(err)
	}
	newVis := m.cols["backlog"]
	if newVis[1].ID != mover {
		t.Errorf("visible order = %v, want the mover at index 1", ids(newVis))
	}
	// And in the FULL lane it must sit immediately before its visible anchor.
	newFull := m.b.LaneTasks("backlog")
	mi, ai := indexOf(newFull, mover), indexOf(newFull, anchor)
	if mi < 0 || ai < 0 || mi >= ai {
		t.Errorf("full lane order wrong: mover at %d, anchor at %d (%v)", mi, ai, ids(newFull))
	}
}

func ids(ts []*board.Task) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.ID
	}
	return out
}

func indexOf(ts []*board.Task, id string) int {
	for i, t := range ts {
		if t.ID == id {
			return i
		}
	}
	return -1
}

// quickReorder (shift+K / shift+J) goes through the same arithmetic.
func TestQuickReorder(t *testing.T) {
	m := New(memstore.New(), Options{})
	m.w, m.h = 140, 40
	m.curLane = m.b.LaneIndex("backlog")
	m.curIdx["backlog"] = 1
	m.recompute()

	before := ids(m.cols["backlog"])
	second := before[1]

	m.quickReorder(-1)
	after := ids(m.cols["backlog"])
	if after[0] != second {
		t.Errorf("K did not raise: %v -> %v", before, after)
	}
	if m.curTask().ID != second {
		t.Errorf("the cursor must follow the card, got %s", m.curTask().ID)
	}

	m.quickReorder(+1)
	if got := ids(m.cols["backlog"]); strings.Join(got, ",") != strings.Join(before, ",") {
		t.Errorf("J did not put it back: %v vs %v", got, before)
	}

	// At the top, K is a no-op with an explanation rather than a silent nothing.
	m.setPos(0)
	m.quickReorder(-1)
	if !strings.Contains(m.status, "already at the top") {
		t.Errorf("status = %q", m.status)
	}
}

// Move mode's arrow arithmetic: the drop index walks 0..len and clamps when the
// lane changes.
func TestMoveModeDropIndexArithmetic(t *testing.T) {
	m := New(memstore.New(), Options{})
	m.w, m.h = 140, 40
	m.curLane = m.b.LaneIndex("backlog")
	m.setPos(0)
	m.recompute()
	m.enterMove()

	if m.mode != modeMove {
		t.Fatal("enterMove did not enter move mode")
	}
	n := len(m.cols["backlog"])

	for i := 0; i < n+5; i++ {
		m.dropIdx = minInt(m.dropSpan(m.dropLane), m.dropIdx+1)
	}
	if m.dropIdx != n {
		t.Errorf("dropIdx ran to %d, want a clamp at %d", m.dropIdx, n)
	}
	for i := 0; i < n+5; i++ {
		m.dropIdx = maxInt(0, m.dropIdx-1)
	}
	if m.dropIdx != 0 {
		t.Errorf("dropIdx floored at %d, want 0", m.dropIdx)
	}

	// Moving to a shorter lane clamps the index into range.
	m.dropIdx = n
	m.dropLane = "backlog"
	m.shiftDropLane(+1) // -> ready, which has one task
	if m.dropIdx > len(m.cols[m.dropLane]) {
		t.Errorf("dropIdx %d exceeds %s (%d slots)", m.dropIdx, m.dropLane, len(m.cols[m.dropLane]))
	}
	// And it cannot walk off either end of the lane vocabulary.
	for i := 0; i < 20; i++ {
		m.shiftDropLane(+1)
	}
	if m.b.LaneIndex(m.dropLane) != len(m.b.Lanes())-1 {
		t.Errorf("dropLane = %s, want the last lane", m.dropLane)
	}
	for i := 0; i < 20; i++ {
		m.shiftDropLane(-1)
	}
	if m.b.LaneIndex(m.dropLane) != 0 {
		t.Errorf("dropLane = %s, want the first lane", m.dropLane)
	}
}

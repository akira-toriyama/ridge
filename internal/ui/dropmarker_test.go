package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The wheel is allowed to move the destination column while a keyboard move
// is in flight, so the two meet routinely. dropY's last fallback reported the
// column's FIRST row for an index scrolled above the fold, so the insertion
// bar sat at the top of the column marking a slot the commit would not use.
// A slot that cannot be shown is not marked — the rule the drag path already
// states for a pointer off the board.
func TestNoDropMarkerForASlotScrolledOutOfView(t *testing.T) {
	const w, h = 240, 24
	m := boardModel(t, w, h)
	m.curLane = m.b.LaneIndex("backlog")
	m.setPos(0)
	m.enterMove()

	dst := m.lay.Col("backlog")
	if dst == nil || len(dst.Cards) == 0 {
		t.Fatal("the fixture backlog column rendered no cards at this size")
	}
	if dst.Hidden == 0 {
		t.Fatalf("the backlog column shows all %d cards at %dx%d; this test needs a fold",
			len(dst.Tasks), w, h)
	}
	m.dropLane, m.dropIdx = "backlog", 0

	// Wheel the destination column down until slot 0 is above the fold.
	for i := 0; i < 5; i++ {
		m.Update(tea.MouseWheelMsg{X: dst.X + 3, Y: dst.Top + 2, Button: tea.MouseWheelDown})
	}
	m.relayout()
	if now := m.lay.Col("backlog"); now == nil || now.Scroll == 0 {
		t.Fatal("the wheel did not scroll the destination column; the setup no longer reaches the state")
	}

	if y, ok := m.lay.dropY("backlog", 0); ok {
		t.Errorf("dropY reported row %d for a slot scrolled out of view, want no marker", y)
	}
	if layer := m.dropLayer(); layer != nil {
		frame := ansiStrip(m.View().Content)
		top := strings.Split(frame, "\n")[m.lay.Col("backlog").Top]
		t.Errorf("an insertion bar is drawn for an off-screen slot:\n%s", top)
	}
}

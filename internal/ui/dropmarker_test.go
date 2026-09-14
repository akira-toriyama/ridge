package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The wheel is allowed to move the destination column while a keyboard move
// is in flight, so the two meet routinely. dropY answers leniently for the
// DRAG — whose index comes from the pointer and is always on screen, and
// which needs the append row — so it reported the column's first row for a
// slot scrolled out of view, and the keyboard move drew an insertion bar at a
// row the commit would not use. modeMove asks slotVisible first.
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

	if m.lay.slotVisible("backlog", 0) {
		t.Error("slotVisible says slot 0 is on screen after the column scrolled past it")
	}
	if layer := m.dropLayer(); layer != nil {
		frame := ansiStrip(m.View().Content)
		top := strings.Split(frame, "\n")[m.lay.Col("backlog").Top]
		t.Errorf("an insertion bar is drawn for an off-screen slot:\n%s", top)
	}
}

// The mirror case: the slot is scrolled BELOW the fold. J (move to bottom)
// followed by wheeling the column back to the top reaches it, and the append
// row after the last VISIBLE card is not the slot the commit will use.
func TestNoDropMarkerForASlotScrolledBelowTheFold(t *testing.T) {
	const w, h = 240, 24
	m := boardModel(t, w, h)
	m.curLane = m.b.LaneIndex("backlog")
	m.setPos(0)
	m.enterMove()

	col := m.lay.Col("backlog")
	if col == nil || len(col.Tasks) < 4 {
		t.Fatalf("the fixture backlog column has %d tasks; this test needs several", len(col.Tasks))
	}
	m.dropLane, m.dropIdx = "backlog", len(col.Tasks)-1
	for i := 0; i < 8; i++ {
		m.Update(tea.MouseWheelMsg{X: col.X + 3, Y: col.Top + 2, Button: tea.MouseWheelUp})
	}
	m.relayout()

	now := m.lay.Col("backlog")
	if now.Scroll != 0 {
		t.Fatalf("the column did not return to the top (scroll=%d)", now.Scroll)
	}
	last := now.Cards[len(now.Cards)-1]
	if last.Idx >= len(now.Tasks)-1 {
		t.Fatalf("every card fits at %dx%d; this test needs cards hidden below the fold", w, h)
	}
	if m.lay.slotVisible("backlog", m.dropIdx) {
		t.Errorf("slotVisible says slot %d is on screen, but the last visible card is %d of %d",
			m.dropIdx, last.Idx, len(now.Tasks)-1)
	}
	if layer := m.dropLayer(); layer != nil {
		t.Error("an insertion bar is drawn for a slot below the fold")
	}
}

package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The wheel and the per-column scroll offset it writes: the two clamps, the
// offset a filter can make stale, and the wheel while the filter input is
// modal. The keyboard's ^d/^u are in ctrlscroll_test.go.

// m.scroll is never reset when the filter shrinks an UNFOCUSED column
// (ensureVisible repairs only the focused lane); buildLayout clamps the
// offset to maxScrollFor per FRAME (layout.go), and that clamp is all that
// stands between a stale offset and a column rendered scrolled past every card
// it still has. Nothing killed a mutant of it: the fixture version of this
// test skipped whenever backlog did not fold, and its guard `Scroll > 0 &&
// Hidden == 0` was a false positive in waiting — Hidden counts only the cards
// below the fold, so any column legitimately scrolled to its end satisfies it.
// This builds the column it needs and asks for the frame.
func TestAdvStaleScrollHidesCardsAfterFiltering(t *testing.T) {
	m := advTallModel(t, 140, 24)
	// Park the cursor in ready: ensureVisible repairs only the FOCUSED lane's
	// offset, so a stale one in backlog reaches the layout clamp untouched.
	m.curLane = m.b.LaneIndex("ready")
	m.setPos(0)
	col := m.lay.Col("backlog")
	for i := 0; i < 20; i++ {
		m.Update(tea.MouseWheelMsg{X: col.X + 4, Y: col.Top + 2, Button: tea.MouseWheelDown})
	}
	m.relayout()
	if m.lay.Col("backlog").Scroll == 0 {
		t.Fatal("setup: twenty wheel-downs did not scroll backlog")
	}
	// Two cards that all fit, with the stale offset still in m.scroll.
	m.applyFilter("label:keep")
	m.relayout()
	after := m.lay.Col("backlog")
	if after == nil {
		t.Fatal("backlog vanished from the layout after filtering")
	}
	if len(after.Tasks) != 2 {
		t.Fatalf("setup: label:keep left %d tasks in backlog, want 2", len(after.Tasks))
	}
	if after.Scroll != 0 || after.Hidden != 0 || len(after.Cards) != 2 {
		t.Errorf("after filtering to 2 cards that fit, backlog renders scroll=%d hidden=%d "+
			"cards=%d — the stale offset survived and the cards above it are unreachable",
			after.Scroll, after.Hidden, len(after.Cards))
	}
}

// While the filter input has the keyboard, a MOUSE CLICK is ignored
// (onMouseDown returns early) but the WHEEL is not, so scrolling changes the
// board under a modal text input. Minor, but it is the kind of asymmetry that
// says the modality was not thought through.
func TestAdvWheelWorksInFilterModeButClicksDoNot(t *testing.T) {
	m := advTallModel(t, 140, 24)
	m.mode = modeFilter
	col := m.lay.Col("backlog")
	before := m.scroll["backlog"]
	m.Update(tea.MouseWheelMsg{X: col.X + 4, Y: col.Top + 2, Button: tea.MouseWheelDown})
	after := m.scroll["backlog"]
	if after != before {
		t.Errorf("the wheel scrolled backlog %d -> %d while the filter input was modal "+
			"(a click at the same spot is correctly ignored)", before, after)
	}
}

// The wheel has two clamps and these two tests are the only guard on either:
// three sibling tests aimed at the shipped fixture, and every one of them
// skipped on every run because no terminal size satisfied its precondition.
// They are gone; these build the board they need instead.
//
// Clamp 1: wheel-down does nothing when nothing is below the fold. Without it,
// one wheel-down on a column where every card already fits scrolls the top card
// off screen, with no cue but a "1 above" hint.
func TestAdvWheelScrollsAColumnThatEntirelyFits(t *testing.T) {
	m := advSmallModel(t, 140, 40)
	col := m.lay.Col("ready")
	if col == nil || col.Hidden != 0 || len(col.Cards) != 3 {
		t.Fatalf("setup: cards=%d hidden=%d", len(col.Cards), col.Hidden)
	}
	m.Update(tea.MouseWheelMsg{X: col.X + 4, Y: 10, Button: tea.MouseWheelDown})

	// On m.scroll, the field the wheel WRITES — not on the laid-out column.
	// buildLayout clamps every offset to maxScrollFor, which is 0 for a column
	// that fits, so the rendered result is identical with or without the
	// handler's guard and an assertion on it cannot fail (observed: deleting
	// `if c.Hidden > 0` left the suite green).
	if got := m.scroll["ready"]; got != 0 {
		t.Errorf("wheel-down on a column with Hidden=0 advanced the stored offset to %d; "+
			"the layout hides it today, but the model now disagrees with what is on screen", got)
	}
	after := m.lay.Col("ready")
	if after.Scroll != 0 || len(after.Cards) != 3 {
		t.Errorf("wheel-down on a column with Hidden=0: scroll %d->%d, cards 3->%d "+
			"(card %q is now off screen)", col.Scroll, after.Scroll, len(after.Cards),
			after.Tasks[0].ID)
	}
}

// Clamp 2: wheel-up stops at the top. Pinned separately because the two clamps
// are independent lines, and a test that only ever scrolls down cannot see this
// one go negative.
func TestAdvWheelUpStopsAtTheTopOfAColumn(t *testing.T) {
	m := advSmallModel(t, 140, 40)
	col := m.lay.Col("ready")
	if col == nil || len(col.Cards) == 0 {
		t.Fatal("setup: no ready cards")
	}
	for i := 0; i < 5; i++ {
		m.Update(tea.MouseWheelMsg{X: col.X + 4, Y: 10, Button: tea.MouseWheelUp})
	}
	if got := m.scroll["ready"]; got != 0 {
		t.Errorf("five wheel-ups at the top of ready left scroll=%d; a negative offset "+
			"indexes before the first card", got)
	}
	if after := m.lay.Col("ready"); len(after.Cards) != len(col.Cards) {
		t.Errorf("scrolling up past the top changed the rendered card count %d -> %d",
			len(col.Cards), len(after.Cards))
	}
}

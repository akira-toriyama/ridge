package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// cardAt resolves the lane from X alone, so every row of the frame — the title
// bar, the filter row, a lane header, the footer — reports the column it sits
// over. Acting on that made a click on the chrome re-point the selection while
// nothing on screen moved, and the open peek went on describing the task the
// user could still see: the next ⏎ or e then opened a DIFFERENT task.
func TestChromeClickLeavesTheSelectionAndThePeekAlone(t *testing.T) {
	const w, h = 240, 40
	m := boardModel(t, w, h)
	m.peekOpen = true
	m.syncPeek()

	far := m.lay.Col("done")
	if far == nil {
		t.Fatal("the fixture board has no done column at this size")
	}
	startLane, startTask := m.curLane, m.curTask()
	if startTask == nil {
		t.Fatal("the board opened with no selected task")
	}
	if m.b.LaneIndex("done") == startLane {
		t.Fatal("this test needs the cursor to start OUTSIDE the column it clicks")
	}

	// Every chrome row over the far column: title bar, filter row, and the
	// rows above the first card (the lane header band).
	for _, y := range []int{0, 1, far.Top - 1, h - 1} {
		m.Update(tea.MouseClickMsg{X: far.X + 3, Y: y, Button: tea.MouseLeft})
		m.Update(tea.MouseReleaseMsg{X: far.X + 3, Y: y, Button: tea.MouseLeft})
		if m.curLane != startLane {
			t.Fatalf("a click at y=%d (chrome) moved the selection to lane %d, want it to stay on %d",
				y, m.curLane, startLane)
		}
	}

	// The column BODY is not chrome: clicking the empty space under the last
	// card focuses that lane, and the peek has to follow the selection there.
	bodyY := far.Bot - 1
	m.Update(tea.MouseClickMsg{X: far.X + 3, Y: bodyY, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: far.X + 3, Y: bodyY, Button: tea.MouseLeft})
	if want := m.b.LaneIndex("done"); m.curLane != want {
		t.Fatalf("a click in the column body left the cursor on lane %d, want %d", m.curLane, want)
	}
	if now := m.curTask(); now != nil {
		if got := m.vp.View(); !strings.Contains(got, now.ID) {
			t.Errorf("the peek still describes %s after the selection moved to %s",
				startTask.ID, now.ID)
		}
	}
}

// The press side refuses every button but the left one, and the drag remembers
// which button lifted the card. The release side did not look: a right-click
// while a card was in flight committed the drop, landing it wherever the
// pointer happened to be.
func TestOnlyTheButtonThatLiftedTheCardCanDropIt(t *testing.T) {
	const w, h = 140, 40
	probe := geometry(t, w, h)
	src, dst := probe.lay.Col("backlog"), probe.lay.Col("ready")
	grab := src.Cards[0]
	mover := src.Tasks[grab.Idx].ID

	m := boardModel(t, w, h)
	m.Update(tea.MouseClickMsg{X: grab.X + 3, Y: grab.Y + 1, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: dst.X + 3, Y: dst.Top + 2, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: dst.X + 3, Y: dst.Top + 2, Button: tea.MouseRight})

	if got := laneOf(m, mover); got != "backlog" {
		t.Errorf("%s landed in %s — a right-button release finished a left-button drag", mover, got)
	}
	if !m.drag.armed {
		t.Error("the foreign release cleared the drag; the left button is still down")
	}

	// The gesture the user actually started must still complete.
	m.Update(tea.MouseReleaseMsg{X: dst.X + 3, Y: dst.Top + 2, Button: tea.MouseLeft})
	if got := laneOf(m, mover); got != "ready" {
		t.Errorf("%s ended in %s — the left-button release did not commit the drop", mover, got)
	}
}

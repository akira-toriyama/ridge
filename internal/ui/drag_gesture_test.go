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
	bodyY := missRowIn(t, m, "done")
	m.Update(tea.MouseClickMsg{X: far.X + 3, Y: bodyY, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: far.X + 3, Y: bodyY, Button: tea.MouseLeft})
	if want := m.b.LaneIndex("done"); m.curLane != want {
		t.Fatalf("a click in the column body left the cursor on lane %d, want %d", m.curLane, want)
	}
	now := m.curTask()
	if now == nil {
		t.Fatal("the done lane has no task to select; this no longer tests the peek")
	}
	if got := m.vp.View(); !strings.Contains(got, now.ID) {
		t.Errorf("the peek still describes %s after the selection moved to %s",
			startTask.ID, now.ID)
	}
}

// The miss path focuses a lane; it does NOT drag the card cursor back into
// view. A column the wheel has scrolled away from its selection is a
// legitimate state (ensureVisible's contract), and re-asserting it here made
// one gesture undo the previous one — but only on an unfocused column, so the
// same click had two outcomes depending on where the cursor already was.
func TestFocusingALaneByClickingItDoesNotUndoItsWheelScroll(t *testing.T) {
	const w, h = 240, 24
	m := boardModel(t, w, h)
	far := m.lay.Col("done")
	if far == nil {
		t.Fatal("the fixture board has no done column at this size")
	}
	if m.b.LaneIndex("done") == m.curLane {
		t.Fatal("this test needs the cursor to start outside the column it scrolls")
	}

	for i := 0; i < 3; i++ {
		m.Update(tea.MouseWheelMsg{X: far.X + 3, Y: far.Top + 2, Button: tea.MouseWheelDown})
	}
	m.relayout()
	scrolled := m.lay.Col("done").Scroll
	if scrolled == 0 {
		t.Fatal("the wheel did not scroll the done column; the setup no longer reaches the state")
	}

	bodyY := missRowIn(t, m, "done")
	m.Update(tea.MouseClickMsg{X: far.X + 3, Y: bodyY, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: far.X + 3, Y: bodyY, Button: tea.MouseLeft})
	m.relayout()

	if got := m.lay.Col("done").Scroll; got != scrolled {
		t.Errorf("clicking the column body moved its scroll from %d to %d — the click undid the wheel",
			scrolled, got)
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

// missRowIn finds a row inside the column body that no card covers — the miss
// path needs one, and which row that is depends on how the cards packed.
func missRowIn(t *testing.T, m *Model, lane string) int {
	t.Helper()
	c := m.lay.Col(lane)
	if c == nil {
		t.Fatalf("no %s column at this size", lane)
	}
	for y := c.Bot - 1; y >= c.Top; y-- {
		if _, _, hit := m.lay.cardAt(c.X+3, y); !hit {
			return y
		}
	}
	t.Fatalf("every row of the %s column body is on a card; this test needs empty space", lane)
	return 0
}

// The threshold is Manhattan >= 2, so a diagonal 1+1 twitch — the single most
// common accidental mouse movement — is a full drag, not a click.
func TestAdvDiagonalOneCellTwitchIsADrag(t *testing.T) {
	m := boardModel(t, 140, 40)
	col := m.lay.Col("backlog")
	if col == nil || len(col.Cards) < 2 {
		t.Fatal("board too small")
	}
	box := col.Cards[1]
	m.Update(tea.MouseClickMsg{X: box.X + 3, Y: box.Y + 1, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: box.X + 4, Y: box.Y + 2, Button: tea.MouseLeft})
	if m.drag.moved {
		t.Errorf("a 1-cell diagonal twitch (dx=1,dy=1, Manhattan 2) armed a real drag")
	}
}

// onKey checks cancelDrag() BEFORE the mode switch, so an Esc meant to dismiss
// the filter input is eaten by a still-armed drag instead.
func TestAdvEscInFilterModeIsEatenByAnArmedDrag(t *testing.T) {
	m := boardModel(t, 140, 40)
	col := m.lay.Col(m.curLaneName())
	if col == nil || len(col.Cards) == 0 {
		t.Fatal("no card")
	}
	box := col.Cards[0]
	m.Update(tea.MouseClickMsg{X: box.X + 3, Y: box.Y + 1, Button: tea.MouseLeft})
	// user presses / while still holding the button, types, then presses esc
	m.Update(tea.KeyPressMsg{Code: '/', Text: "/"})
	if m.mode != modeFilter {
		t.Fatal("did not enter filter mode")
	}
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})
	if m.mode == modeFilter {
		t.Errorf("esc was swallowed by the armed drag; the filter input is still modal "+
			"(drag.armed=%v cancelled=%v)", m.drag.armed, m.drag.cancelled)
	}
}

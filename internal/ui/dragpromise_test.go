package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// The drop promise and the drop itself must come from one predicate.
//
// Both failures below shipped: the release consulted dropTarget() while the
// insertion bar and the status line were driven off dragState's cached
// dropLane/dropIdx, so the frame promised drops the release refused — and the
// peek, which is painted ABOVE the insertion bar, hid the column a release
// still committed into.

// dragFrom presses on a column's first card, moves the pointer to (x,y) and
// returns the dragged task's id. The press-to-move gap clears dragThreshold.
func dragFrom(t *testing.T, m *Model, lane string, x, y int) string {
	t.Helper()
	c := m.lay.Col(lane)
	if c == nil || len(c.Cards) == 0 {
		t.Fatalf("column %q has no cards to drag", lane)
	}
	box := c.Cards[0]
	id := c.Tasks[box.Idx].ID
	m.Update(tea.MouseClickMsg{X: box.X + 3, Y: box.Y + 1, Button: tea.MouseLeft})
	// Arm past the Chebyshev threshold first, THEN travel to the probe point:
	// a probe one cell from the press is a click, and this helper is used with
	// points deliberately close to the card it grabbed.
	m.Update(tea.MouseMotionMsg{X: box.X + 3 + dragThreshold, Y: box.Y + 1 + dragThreshold, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft})
	if !m.drag.moved {
		t.Fatalf("the pointer move to (%d,%d) did not arm a drag", x, y)
	}
	return id
}

// A release over the open peek must not commit into the column hidden under
// it. The peek is zPeek=80 and the drop indicator zDrop=50, so the user is
// shown no insertion mark at all — the card would simply land in a lane that
// was never on screen.
func TestDropOnThePeekDoesNotCommitIntoTheHiddenColumn(t *testing.T) {
	m := boardModel(t, 240, 60)
	m.peekOpen = true
	m.syncPeek()
	m.relayout()

	px, py, _, _ := m.peekBox()
	// A point inside the peek that is also horizontally inside a real column:
	// without the guard this resolves through m.lay to whatever is underneath.
	hidden, ok := m.lay.laneAtX(px + 4)
	if !ok {
		t.Skip("no column lies under the peek at this size")
	}
	id := dragFrom(t, m, "backlog", px+4, py+6)
	if from := m.b.Task(id).Status; from == hidden {
		t.Skip("the dragged card already lives in the hidden column")
	}
	before := m.b.Task(id).Status

	m.Update(tea.MouseReleaseMsg{X: px + 4, Y: py + 6, Button: tea.MouseLeft})

	if got := m.b.Task(id).Status; got != before {
		t.Errorf("a release on the peek moved %s from %s to %s — it committed into the column hidden under the panel",
			id, before, got)
	}
	if !strings.Contains(m.status, "off the board") {
		t.Errorf("a release on the peek must say it did not land; status was %q", m.status)
	}
}

// While the pointer is over the peek there is nothing to promise: the drop
// indicator must be gone and the status must not say "release to drop".
func TestNoDropPromiseWhileThePointerIsOverThePeek(t *testing.T) {
	m := boardModel(t, 240, 60)
	m.peekOpen = true
	m.syncPeek()
	m.relayout()

	px, py, _, _ := m.peekBox()
	if _, ok := m.lay.laneAtX(px + 4); !ok {
		t.Skip("no column lies under the peek at this size")
	}
	dragFrom(t, m, "backlog", px+4, py+6)

	if l := m.dropLayer(); l != nil {
		t.Error("the insertion bar is drawn under the peek, where it is invisible and the release cancels")
	}
	if s := m.statusLine(); strings.Contains(s, "release to drop") {
		t.Errorf("the status promises a drop the release will cancel: %q", s)
	}
}

// Every point the release would refuse must render as a refusal. Before the
// fix the y=Bot-1 and y=Bot frames were byte-identical with opposite outcomes.
func TestTheDropPromiseAgreesWithTheReleaseEverywhere(t *testing.T) {
	m := boardModel(t, 240, 60)
	c := m.lay.Col("backlog")
	if c == nil || len(c.Cards) == 0 {
		t.Fatal("expected a populated backlog column")
	}
	x := c.X + 3

	// The card band, the row under it, the chrome above it, the footer, and
	// the gutter between two columns.
	// c.Bot and m.h-1 were the same cell, so the sweep spent two of its five
	// probes on one point.
	second := m.lay.Cols[1]
	points := []struct {
		name string
		x, y int
	}{
		{"inside the card band", x, c.Top + 1},
		{"the last usable row", x, c.Bot - 1},
		{"one row below the band", x, c.Bot},
		{"the header rows", x, rowColHdr},
		{"the footer", x, m.h - 1},
		{"the gutter between columns", c.X + c.W, c.Top + 1},
		{"a second column's band", second.X + 3, second.Top + 1},
	}

	for _, p := range points {
		m := boardModel(t, 240, 60)
		dragFrom(t, m, "backlog", p.x, p.y)

		wantLane, wouldDrop := m.dropTarget(p.x, p.y)
		gotBar := m.dropLayer() != nil
		status := ansiStrip(m.statusLine())
		promises := strings.Contains(status, "release to drop")

		// The lane the frame NAMES has to be the lane the release would use.
		// Gating on the boolean alone stayed green while the bar marked and
		// the status announced a different column.
		if wouldDrop && !strings.Contains(status, wantLane) {
			t.Errorf("%s (%d,%d): the release would drop into %q but the status says %q",
				p.name, p.x, p.y, wantLane, status)
		}
		if wouldDrop {
			if l := m.dropLayer(); l != nil {
				if col := m.lay.Col(wantLane); col != nil && l.GetX() != col.X {
					t.Errorf("%s (%d,%d): the insertion bar is drawn at x=%d, but %q starts at x=%d",
						p.name, p.x, p.y, l.GetX(), wantLane, col.X)
				}
			}
		}

		if gotBar != wouldDrop {
			t.Errorf("%s (%d,%d): dropTarget=%v but an insertion bar is %v — the frame and the release disagree",
				p.name, p.x, p.y, wouldDrop, gotBar)
		}
		if promises != wouldDrop {
			t.Errorf("%s (%d,%d): dropTarget=%v but the status promises a drop=%v",
				p.name, p.x, p.y, wouldDrop, promises)
		}
	}
}

// The divergence the render-time predicate exists for: nothing says a motion
// event precedes every frame. A resize (or the auto-scroll's own relayout)
// moves the columns under a pointer that has not moved, so values cached at
// motion time now name a different lane than the release would choose.
func TestTheDropPromiseFollowsTheColumnsAcrossAResize(t *testing.T) {
	m := boardModel(t, 240, 60)
	if len(m.lay.Cols) < 3 {
		t.Skip("need at least three visible columns")
	}
	// A point deep in the third column at this width.
	third := m.lay.Cols[2]
	x, y := third.X+3, third.Top+1
	dragFrom(t, m, "backlog", x, y)

	cached := m.drag.dropLane
	if cached != third.Lane.Name {
		t.Fatalf("setup: the drag tracked %q, want %q", cached, third.Lane.Name)
	}

	// Widen the terminal WITHOUT another mouse event. The columns grow, so the
	// same x now falls in an earlier lane.
	m.w = 400
	m.relayout()

	want, ok := m.dropTarget(x, y)
	if !ok {
		t.Skip("the point left the board entirely at the new width")
	}
	if want == cached {
		t.Skip("the resize did not move this point into a different lane")
	}

	status := ansiStrip(m.statusLine())
	if !strings.Contains(status, want) {
		t.Errorf("after the resize the release would drop into %q, but the status still says %q (cached %q)",
			want, status, cached)
	}
	if l := m.dropLayer(); l != nil {
		if col := m.lay.Col(want); col != nil && l.GetX() != col.X {
			t.Errorf("the insertion bar is still drawn over the old column: x=%d, %q starts at x=%d",
				l.GetX(), want, col.X)
		}
	}
}

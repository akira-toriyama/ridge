package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
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
	m.applyFilter("label:bbq")
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
	if _, _, err := m.commitMove(mover, "backlog", "backlog", 2); err != nil {
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

// Move mode's extremes, driven by the letter keys through the real Update
// path. The letters are the PRIMARY binding: macOS Terminal never delivers the
// ctrl+arrow aliases, so if K/J/H/L regress the gesture is simply gone there.
func TestMoveModeLettersReachTheExtremes(t *testing.T) {
	m := New(memstore.New(), Options{})
	m.w, m.h = 140, 40
	m.curLane = m.b.LaneIndex("backlog")
	m.setPos(1)
	m.recompute()
	m.relayout()
	m.enterMove()
	if m.mode != modeMove {
		t.Fatal("enterMove did not enter move mode")
	}

	press := func(r rune) {
		m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}

	press('J')
	if want := m.dropSpan(m.dropLane); m.dropIdx != want {
		t.Errorf("J: dropIdx = %d, want the bottom (%d)", m.dropIdx, want)
	}
	press('K')
	if m.dropIdx != 0 {
		t.Errorf("K: dropIdx = %d, want 0", m.dropIdx)
	}
	press('L')
	if got := m.b.LaneIndex(m.dropLane); got != len(m.b.Lanes())-1 {
		t.Errorf("L: dropLane = %s (index %d), want the last lane", m.dropLane, got)
	}
	press('H')
	if got := m.b.LaneIndex(m.dropLane); got != 0 {
		t.Errorf("H: dropLane = %s (index %d), want the first lane", m.dropLane, got)
	}
	if m.mode != modeMove {
		t.Errorf("the extremes must not leave move mode, got mode %v", m.mode)
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

// The status line and doc.go both promise "esc restores". The BOARD is restored
// (nothing was mutated), but the CURSOR is left wherever the arrows parked the
// drop target: cancel never puts the selection back on the card you lifted.
func TestAdvMoveModeCancelLeavesTheCursorOnTheWrongTask(t *testing.T) {
	m := boardModel(t, 140, 40)
	m.curLane = m.b.LaneIndex("backlog")
	m.setPos(0)
	lifted := m.curTask()
	if lifted == nil {
		t.Fatal("no card to lift")
	}
	m.enterMove()
	// place it two lanes over and two slots down
	m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	m.Update(tea.KeyPressMsg{Code: tea.KeyDown})
	m.Update(tea.KeyPressMsg{Code: tea.KeyEscape})

	if m.mode != modeNormal {
		t.Fatalf("esc did not leave move mode")
	}
	if got := m.curTask(); got == nil || got.ID != lifted.ID {
		gotID := "<nil>"
		if got != nil {
			gotID = got.ID
		}
		t.Errorf("after esc the selection is %s, want the lifted card %s (lane %s idx %d)",
			gotID, lifted.ID, m.curLaneName(), m.curPos())
	}
}

// A mouse drag can be started and then a keyboard move mode entered on top of
// it, giving one card two owners. The reverse direction IS guarded
// (onMouseDown refuses while mode==modeMove); this direction is not, so the
// release commits one move and the following Enter commits a second.
func TestAdvKeyboardMoveModeCanBeEnteredMidDrag(t *testing.T) {
	m := boardModel(t, 140, 40)
	src := m.lay.Col("backlog")
	dst := m.lay.Col("ready")
	if src == nil || dst == nil || len(src.Cards) < 2 {
		t.Fatal("board too small")
	}
	box := src.Cards[1]
	id := src.Tasks[box.Idx].ID

	m.Update(tea.MouseClickMsg{X: box.X + 3, Y: box.Y + 1, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: dst.X + 8, Y: dst.Top + 2, Button: tea.MouseLeft})
	if !m.drag.moved {
		t.Fatal("drag did not arm")
	}
	// The user presses `m` while still holding the button.
	m.Update(tea.KeyPressMsg{Code: 'm', Text: "m"})
	if m.mode == modeMove && m.drag.armed {
		t.Errorf("card %s now has two owners: drag armed AND move mode active "+
			"(moveID=%s dropLane=%s / drag.id=%s dropLane=%s)",
			id, m.moveID, m.dropLane, m.drag.id, m.drag.dropLane)
	}
}

// shift+J/K reorder inside a FILTERED column. The visible neighbour is not the
// board neighbour, so "lower by one" must land immediately after the next
// VISIBLE card and must not jump over hidden ones... but it must also actually
// change the board order.
func TestAdvQuickReorderUnderAFilterMovesExactlyOneVisibleSlot(t *testing.T) {
	m := boardModel(t, 140, 40)
	m.applyFilter("lane:backlog")
	m.curLane = m.b.LaneIndex("backlog")
	m.setPos(0)
	vis := append([]*board.Task(nil), m.cols["backlog"]...)
	if len(vis) < 3 {
		t.Fatal("need >=3 visible backlog tasks")
	}
	first, second := vis[0].ID, vis[1].ID
	m.quickReorder(+1)
	got := m.cols["backlog"]
	if got[0].ID != second || got[1].ID != first {
		t.Errorf("after shift+J the visible order is %s,%s; want %s,%s",
			got[0].ID, got[1].ID, second, first)
	}
}

// reference is what a human means by "drop this card into slot d of lane L, as
// the lane is currently DISPLAYED".
func advReference(cols map[string][]string, id, from, to string, dispIdx int) map[string][]string {
	out := map[string][]string{}
	for k, v := range cols {
		out[k] = append([]string(nil), v...)
	}
	// remove
	src := out[from]
	for i, x := range src {
		if x == id {
			src = append(src[:i], src[i+1:]...)
			break
		}
	}
	out[from] = src
	// insert: dispIdx counts the destination AS DISPLAYED, i.e. still holding
	// the moving card when from==to.
	idx := dispIdx
	if from == to && dispIdx > 0 {
		// the vacated slot above absorbs one
		fromIdx := -1
		for i, x := range cols[from] {
			if x == id {
				fromIdx = i
			}
		}
		if dispIdx > fromIdx {
			idx = dispIdx - 1
		}
	}
	dst := out[to]
	if idx > len(dst) {
		idx = len(dst)
	}
	if idx < 0 {
		idx = 0
	}
	dst = append(dst[:idx:idx], append([]string{id}, dst[idx:]...)...)
	out[to] = dst
	return out
}

func TestAdvMoveArithmeticAgainstAReference(t *testing.T) {
	lanes := []string{"inbox", "backlog", "ready", "in-progress", "done", "icebox"}
	for _, from := range lanes {
		for _, to := range lanes {
			// logicModel: this loop reads m.cols and commits moves; it never
			// touches m.lay, and 1,361 layouts cost 39s under -race.
			base := logicModel(t, 140, 40)
			src := base.cols[from]
			if len(src) == 0 {
				continue
			}
			for fi := range src {
				for di := 0; di <= len(base.cols[to]); di++ {
					m := logicModel(t, 140, 40)
					id := m.cols[from][fi].ID
					before := map[string][]string{}
					for _, l := range lanes {
						before[l] = ids(m.cols[l])
					}
					want := advReference(before, id, from, to, di)
					if _, _, err := m.commitMove(id, from, to, di); err != nil {
						t.Fatalf("%s[%d] -> %s[%d]: %v", from, fi, to, di, err)
					}
					for _, l := range lanes {
						got := ids(m.cols[l])
						if strings.Join(got, ",") != strings.Join(want[l], ",") {
							t.Errorf("%s[%d] -> %s[%d]: lane %s is %v, want %v",
								from, fi, to, di, l, got, want[l])
						}
					}
				}
			}
		}
	}
}

func TestAdvMoveModeAcrossTheWholeStrip(t *testing.T) {
	m := boardModel(t, 90, 40) // only ~3 columns fit
	m.curLane = m.b.LaneIndex("backlog")
	m.setPos(0)
	id := m.curTask().ID
	m.enterMove()
	for i := 0; i < 5; i++ {
		m.Update(tea.KeyPressMsg{Code: tea.KeyRight})
	}
	wantLane := m.dropLane
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.b.Task(id).Status; got != wantLane {
		t.Errorf("committed into %s, want %s", got, wantLane)
	}
	if m.lay.Col(wantLane) == nil {
		t.Errorf("committed into %s but that lane is not in the visible strip "+
			"(laneOff=%d visible=%d)", wantLane, m.laneOff, m.lay.Visible)
	}
}

func TestAdvMoveIntoAnEmptyFilteredLaneAppendsToTheRealEnd(t *testing.T) {
	m := boardModel(t, 140, 40)
	// hide everything in backlog, then move a ready card into backlog slot 0.
	m.applyFilter("lane:ready")
	if len(m.cols["backlog"]) != 0 {
		t.Fatal("backlog should be empty under this filter")
	}
	full := ids(m.b.LaneTasks("backlog"))
	if len(full) < 2 {
		t.Fatal("need a populated backlog")
	}
	id := m.cols["ready"][0].ID
	if _, _, err := m.commitMove(id, "ready", "backlog", 0); err != nil {
		t.Fatal(err)
	}
	got := ids(m.b.LaneTasks("backlog"))
	if got[0] != id {
		t.Errorf("dropped into slot 0 of a (filtered-empty) backlog; the card landed at "+
			"index %d of the real lane %v — the gesture said TOP, the board says BOTTOM",
			indexOfStr(got, id), got)
	}
}

func indexOfStr(ss []string, s string) int {
	for i, x := range ss {
		if x == s {
			return i
		}
	}
	return -1
}

package ui

import (
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// T. scroll offsets survive a filter that makes them meaningless

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

// U. dropping into an empty column

func TestAdvDragIntoAnEmptyColumn(t *testing.T) {
	m := advSmallModel(t, 140, 40)
	if len(m.cols["inbox"]) != 0 {
		t.Fatal("setup: advSmallBoard files nothing in inbox")
	}
	src := m.lay.Col("backlog")
	dst := m.lay.Col("inbox")
	if src == nil || dst == nil || len(src.Cards) < 1 {
		t.Fatal("board too small")
	}
	box := src.Cards[0]
	id := src.Tasks[box.Idx].ID
	m.Update(tea.MouseClickMsg{X: box.X + 3, Y: box.Y + 1, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: dst.X + 8, Y: dst.Top + 3, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: dst.X + 8, Y: dst.Top + 3, Button: tea.MouseLeft})
	if got := m.b.Task(id).Status; got != "inbox" {
		t.Errorf("dropped %s into the empty inbox column; it is in %s", id, got)
	}
}

// Dropping BELOW the last card of a short column (in the empty space under it)
// must append, not land at slot 0.
func TestAdvDragBelowTheLastCardAppends(t *testing.T) {
	m := boardModel(t, 140, 40)
	src := m.lay.Col("backlog")
	dst := m.lay.Col("ready")
	if src == nil || dst == nil || len(src.Cards) < 1 || len(dst.Cards) < 1 {
		t.Fatal("board too small")
	}
	box := src.Cards[0]
	id := src.Tasks[box.Idx].ID
	last := dst.Cards[len(dst.Cards)-1]
	deepY := last.Y + last.H + 3 // empty space well below the last card
	if deepY >= dst.Bot {
		t.Fatalf("setup: no empty space below the last ready card at 140x40 (deepY=%d bot=%d)", deepY, dst.Bot)
	}
	m.Update(tea.MouseClickMsg{X: box.X + 3, Y: box.Y + 1, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: dst.X + 8, Y: deepY, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: dst.X + 8, Y: deepY, Button: tea.MouseLeft})
	got := m.b.LaneTasks("ready")
	if got[len(got)-1].ID != id {
		var ids []string
		for _, x := range got {
			ids = append(ids, x.ID)
		}
		t.Errorf("dropped %s in the empty space below every ready card; lane is now %v",
			id, ids)
	}
}

// V. the drag ghost when the source card is scrolled off

func TestAdvDragSurvivesTheSourceScrollingAway(t *testing.T) {
	const w, h = 140, 24
	m := advTallModel(t, w, h)
	src := m.lay.Col("backlog")
	box := src.Cards[0]
	id := src.Tasks[box.Idx].ID
	m.Update(tea.MouseClickMsg{X: box.X + 3, Y: box.Y + 1, Button: tea.MouseLeft})
	// drag downward hard so the edge auto-scroll kicks in
	for y := box.Y + 2; y < h; y++ {
		m.Update(tea.MouseMotionMsg{X: box.X + 3, Y: y, Button: tea.MouseLeft})
	}
	m.Update(tea.MouseReleaseMsg{X: box.X + 3, Y: h - 3, Button: tea.MouseLeft})
	if m.b.Task(id) == nil {
		t.Fatalf("%s vanished", id)
	}
	if m.b.Task(id).Status != "backlog" {
		t.Errorf("a straight-down drag inside backlog moved %s to %s",
			id, m.b.Task(id).Status)
	}
}

// W. filter-mode keys that leak

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

// X. `>` jump pins: TestJumpBackPinsOnlyWhatTheFilterHides (frametruth_test.go)
// holds the invariant; the copy that lived here filtered to lane:backlog while
// its comment said "unfiltered", and went red the moment the fixture moved a
// blocker out of that lane — a false positive, since pinning a filter-hidden
// jump target is the correct behaviour.

// Z. the bubbletea v2 key-string trap, for every binding that can fall into it

// key.Matches compares Key.String(), so a binding written with the wrong
// spelling compiles, runs, and silently never fires — the failure mode that
// makes WithKeys(" ") a no-op where WithKeys("space") works. Every binding whose
// name is not a bare letter is at risk, so each one is driven with the message a
// terminal would actually produce.
//
// (This replaces a test that only logged a complaint about a scratch file which
// is not in the tree. It asserted nothing, which is precisely what it accused
// that file of.)
func TestKeyBindingsMatchTheirRealKeyStrings(t *testing.T) {
	k := defaultKeys()
	cases := []struct {
		msg  tea.KeyPressMsg
		want key.Binding
		name string
	}{
		{tea.KeyPressMsg{Code: tea.KeySpace}, k.Peek, "space"},
		{tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModShift}, k.Graph, "shift+space"},
		{tea.KeyPressMsg{Code: tea.KeyEnter}, k.Move, "enter"},
		{tea.KeyPressMsg{Code: tea.KeyEscape}, k.Cancel, "esc"},
		{tea.KeyPressMsg{Code: tea.KeyTab}, k.NextCol, "tab"},
		{tea.KeyPressMsg{Code: tea.KeyUp, Mod: tea.ModCtrl}, k.MoveTop, "ctrl+up"},
		{tea.KeyPressMsg{Code: tea.KeyDown, Mod: tea.ModCtrl}, k.MoveBottom, "ctrl+down"},
		{tea.KeyPressMsg{Code: tea.KeyLeft, Mod: tea.ModCtrl}, k.MoveFirst, "ctrl+left"},
		{tea.KeyPressMsg{Code: tea.KeyRight, Mod: tea.ModCtrl}, k.MoveLast, "ctrl+right"},
		{tea.KeyPressMsg{Code: 'K', Text: "K"}, k.MoveTop, "K (move)"},
		{tea.KeyPressMsg{Code: 'J', Text: "J"}, k.MoveBottom, "J (move)"},
		{tea.KeyPressMsg{Code: 'H', Text: "H"}, k.MoveFirst, "H (move)"},
		{tea.KeyPressMsg{Code: 'L', Text: "L"}, k.MoveLast, "L (move)"},
		{tea.KeyPressMsg{Code: 'K', Text: "K"}, k.QuickUp, "K"},
		{tea.KeyPressMsg{Code: 'J', Text: "J"}, k.QuickDown, "J"},
		{tea.KeyPressMsg{Code: 'H', Text: "H"}, k.LaneBack, "H"},
		{tea.KeyPressMsg{Code: 'L', Text: "L"}, k.LaneFwd, "L"},
		{tea.KeyPressMsg{Code: 'M', Text: "M"}, k.Mouse, "M"},
		{tea.KeyPressMsg{Code: 'o', Text: "o"}, k.GraphOrient, "o (graph)"},
		{tea.KeyPressMsg{Code: 'o', Text: "o"}, k.Sort, "o (table)"},
	}
	for _, tc := range cases {
		if !key.Matches(tc.msg, tc.want) {
			t.Errorf("%s: key.Matches saw String()=%q, which is in none of %v",
				tc.name, tc.msg.String(), tc.want.Keys())
		}
	}

	// shift+space opens the dep graph; plain space opens the peek. If the two
	// ever collapsed to the same string, one gesture would shadow the other.
	//
	// And they DO collapse on a terminal that does not speak the Kitty keyboard
	// protocol: a legacy terminal cannot encode a modified space at all, so
	// shift+space arrives as a bare space and the graph is unreachable. That is
	// not hypothetical — it is what the first person to try this POC hit. The
	// binding therefore carries "S" as the portable alias, and View() asks for
	// keyboard enhancements so the pretty gesture works where it can.
	shifted := tea.KeyPressMsg{Code: tea.KeySpace, Mod: tea.ModShift}
	if key.Matches(shifted, k.Peek) {
		t.Error("shift+space also matches the peek binding; the two gestures collide")
	}
	if key.Matches(tea.KeyPressMsg{Code: tea.KeySpace}, k.Graph) {
		t.Error("plain space matches the graph binding; the two gestures collide")
	}
	if !key.Matches(tea.KeyPressMsg{Code: 'S', Text: "S"}, k.Graph) {
		t.Error("S must open the graph too: it is the only way in on a terminal " +
			"that cannot encode shift+space")
	}

	// `o` deliberately has two owners — sort in the table, orientation in the
	// graph — on the licence keys.go states: it is ORDER, "how this view
	// arranges what it shows", and the two views are never on screen together.
	// The routing that keeps them apart is onGraphKey being reached before
	// onNormalKey (model.go), so the pairing is pinned here rather than left to
	// read as an accident.
	if k.Sort.Help().Desc == k.GraphOrient.Help().Desc {
		t.Error("the two owners of `o` describe themselves identically; " +
			"the help overlay would list the same row twice")
	}
}

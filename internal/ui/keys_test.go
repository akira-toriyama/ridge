package ui

import (
	"testing"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

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

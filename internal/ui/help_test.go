package ui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/key"
)

// Esc takes off the TOP layer and nothing else. The board's Cancel used to have
// no fullHelp case at all, so `?` then Esc left the overlay up AND quietly
// closed whatever sat under it — the screen looked frozen while state was
// being destroyed behind the thing the user was reading.
func TestEscClosesHelpBeforeAnythingUnderIt(t *testing.T) {
	t.Run("help alone", func(t *testing.T) {
		m := boardModel(t, 240, 50)
		press(m, "?")
		if !m.fullHelp {
			t.Fatal("? did not open the help overlay")
		}
		press(m, "esc")
		if m.fullHelp {
			t.Error("esc left the help overlay up")
		}
	})

	t.Run("help over a peek", func(t *testing.T) {
		m := boardModel(t, 240, 50)
		press(m, "space", "?")
		if !m.peekOpen || !m.fullHelp {
			t.Fatalf("setup: peek=%v help=%v", m.peekOpen, m.fullHelp)
		}
		press(m, "esc")
		if m.fullHelp {
			t.Error("esc left the help overlay up")
		}
		if !m.peekOpen {
			t.Error("esc reached past the help overlay and closed the peek under it")
		}
	})

	t.Run("help over a dep tree", func(t *testing.T) {
		m := boardModel(t, 240, 50)
		press(m, "space", "t", "?")
		if !m.treeOpen || !m.fullHelp {
			t.Fatalf("setup: tree=%v help=%v", m.treeOpen, m.fullHelp)
		}
		press(m, "esc")
		if m.fullHelp {
			t.Error("esc left the help overlay up")
		}
		if !m.treeOpen {
			t.Error("esc closed the tree under the help overlay")
		}
	})

	t.Run("help over a filter", func(t *testing.T) {
		m := boardModel(t, 240, 50)
		m.applyFilter("lane:backlog")
		if m.qRaw == "" {
			t.Fatal("setup: filter did not stick")
		}
		press(m, "?", "esc")
		if m.fullHelp {
			t.Error("esc left the help overlay up")
		}
		if m.qRaw == "" {
			t.Error("esc cleared the filter under the help overlay")
		}
	})
}

// The ladder Esc walked before the fix must still walk the same way once the
// help overlay is out of the picture: closing help must not have become a
// swallow-everything case.
func TestEscLadderWithoutHelp(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "space", "t")
	if !m.treeOpen || !m.peekOpen {
		t.Fatalf("setup: tree=%v peek=%v", m.treeOpen, m.peekOpen)
	}

	press(m, "esc")
	if m.treeOpen {
		t.Error("esc did not close the tree first")
	}
	if !m.peekOpen {
		t.Error("esc closed the peek in the same press as the tree")
	}

	press(m, "esc")
	if m.peekOpen {
		t.Error("esc did not close the peek second")
	}

	m.applyFilter("lane:backlog")
	press(m, "esc")
	if m.qRaw != "" {
		t.Error("esc did not clear the filter third")
	}
}

// The graph already closed help on Esc before this change; assert it still
// does, and that closing help there does not also drop out of the graph.
func TestEscClosesHelpInsideTheGraph(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.openGraph()
	if m.view != viewGraph {
		t.Fatal("setup: graph did not open")
	}
	press(m, "?", "esc")
	if m.fullHelp {
		t.Error("esc left the help overlay up in the graph")
	}
	if m.view != viewGraph {
		t.Error("esc closing the help also left the graph")
	}
}

// `?` is the canonical key list, built from the same bindings the Update path
// matches on so it "cannot advertise a key that does not work". The converse
// was never checked: it could, and did, OMIT keys that do work.
func TestHelpListsEveryKeyItsSectionsModesHandle(t *testing.T) {
	k := defaultKeys()
	secs := k.HelpSections(false)

	byTitle := map[string]helpSection{}
	for _, s := range secs {
		byTitle[s.title] = s
	}

	lists := func(sec helpSection, want key.Binding) bool {
		for _, g := range sec.groups {
			for _, b := range g {
				if b.Help().Key == want.Help().Key {
					return true
				}
			}
		}
		return false
	}

	// onMoveKey and onGraphKey both handle the arrows; neither section listed
	// them. In the graph that was sharp: re-rooting answers "move the
	// selection first" and no listed key moved it.
	for _, title := range []string{"move mode", "graph"} {
		sec, ok := byTitle[title]
		if !ok {
			t.Fatalf("no %q section in the help overlay", title)
		}
		for _, b := range []key.Binding{k.Up, k.Down, k.Left, k.Right} {
			if !lists(sec, b) {
				t.Errorf("the %q section omits %q, which its own handler acts on", title, b.Help().Key)
			}
		}
	}
}

// shift+space cannot be encoded without the Kitty keyboard protocol, so on a
// plain terminal `S` is the only way into the graph. The overlay renders
// Help(), which can never show a WithKeys alias, so it advertised only the
// half that does not work.
func TestHelpAdvertisesThePortableGraphKey(t *testing.T) {
	frame := strings.Join(dumpFrame(t, 240, 50, "help"), "\n")
	if !strings.Contains(frame, "S") || !strings.Contains(frame, "dep graph") {
		t.Fatal("the help overlay does not mention the graph at all")
	}
	for _, line := range strings.Split(frame, "\n") {
		if !strings.Contains(line, "dep graph") {
			continue
		}
		if !strings.Contains(line, "/S") {
			t.Errorf("the graph entry advertises only the key a plain terminal cannot send: %q", strings.TrimSpace(line))
		}
		return
	}
}

// ⏎/m lifts the card on the bare board and opens the EDIT overlay with the
// peek open or on a table row. The one canonical list has to say which.
func TestHelpNamesWhatEnterWillActuallyDo(t *testing.T) {
	k := defaultKeys()
	find := func(enterEdits bool) string {
		for _, g := range k.HelpSections(enterEdits)[0].groups {
			for _, b := range g {
				if b.Help().Key == "⏎/m" {
					return b.Help().Desc
				}
			}
		}
		return ""
	}
	if got := find(false); got != "move mode" {
		t.Errorf("on the bare board ⏎/m lifts the card, help says %q", got)
	}
	if got := find(true); got == "move mode" {
		t.Error("with the peek open or in Table view ⏎/m opens the edit overlay, but help still says move mode")
	}
}

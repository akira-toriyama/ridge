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

	// Every MODE-SPECIFIC key its handler acts on. The three globals — `?`,
	// `q` and `^c` — are deliberately listed once, in normal mode, rather than
	// repeated in every section; and in the graph `⇧space/S` is an alias for
	// GraphRoot, already listed as `⏎ re-root here`, so a second row carrying
	// the "dep graph" description would make the overlay worse, not better.
	want := map[string][]key.Binding{
		// onMoveKey (movemode.go)
		"move mode": {k.Up, k.Down, k.Left, k.Right, k.Commit, k.Cancel,
			k.MoveTop, k.MoveBottom, k.MoveFirst, k.MoveLast},
		// onGraphKey (model.go)
		"graph": {k.Up, k.Down, k.Left, k.Right, k.GraphRoot, k.GraphRadius,
			k.JumpBack, k.PeekScroll, k.Map, k.View, k.Cancel},
		// onMapKey (depmapview.go)
		"dep map": {k.Up, k.Down, k.Left, k.Right, k.MapGraph, k.MapScope,
			k.PeekScroll, k.Map, k.View, k.Cancel},
	}
	for title, bindings := range want {
		sec, ok := byTitle[title]
		if !ok {
			t.Fatalf("no %q section in the help overlay", title)
		}
		for _, b := range bindings {
			if !lists(sec, b) {
				t.Errorf("the %q section omits %q, which its own handler acts on", title, b.Help().Key)
			}
		}
	}

	// And the globals are reachable from the listing at all.
	normal, ok := byTitle["normal mode"]
	if !ok {
		t.Fatal("no normal-mode section")
	}
	for _, b := range []key.Binding{k.Help, k.Quit} {
		if !lists(normal, b) {
			t.Errorf("normal mode omits the global key %q", b.Help().Key)
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
//
// Driven through the RENDERED FRAME, not by calling HelpSections directly:
// asking the keymap proves the parameter works but leaves the call site free
// to pass a constant, which is exactly the mutation that survived the first
// version of this test.
func TestHelpNamesWhatEnterWillActuallyDo(t *testing.T) {
	descFor := func(t *testing.T, setup func(*Model)) string {
		t.Helper()
		m := boardModel(t, 240, 50)
		setup(m)
		m.fullHelp = true
		for _, line := range strings.Split(ansiStrip(m.View().Content), "\n") {
			if i := strings.Index(line, "⏎/m"); i >= 0 {
				rest := strings.TrimSpace(line[i+len("⏎/m"):])
				if j := strings.Index(rest, "  "); j > 0 {
					rest = rest[:j]
				}
				return strings.TrimSpace(rest)
			}
		}
		return ""
	}

	if got := descFor(t, func(*Model) {}); got != "move mode" {
		t.Errorf("on the bare board ⏎/m lifts the card, the overlay says %q", got)
	}
	if got := descFor(t, func(m *Model) { m.peekOpen = true; m.syncPeek() }); got == "move mode" {
		t.Error("with the peek open ⏎/m opens the edit overlay, but the overlay still says move mode")
	}
	if got := descFor(t, func(m *Model) { m.view = viewTable }); got == "move mode" {
		t.Error("in Table view ⏎/m opens the edit overlay, but the overlay still says move mode")
	}
}

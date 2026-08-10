package ui

import "testing"

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

package ui

import (
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The radius bounds what the ego graph includes, not how it layers what it
// included. With the layers capped at the radius, a node reachable in one hop
// but also at the end of a three-hop path sat on the outermost layer beside
// its own dependency, and the edge between them was reported as cyclic — on
// ridge-test (100 tasks, no cycle) every graph at radius 2 said "1 cyclic
// edge(s) not drawn" (t-z9nf).
func TestEgoLayersFollowTheLongestPathPastTheRadius(t *testing.T) {
	// f → a → b → c, and f → c directly: c is one hop from f and three hops
	// down the long way.
	b := board.NewBoard([]*board.Task{
		{ID: "f", Title: "f", Status: "backlog"},
		{ID: "a", Title: "a", Status: "backlog", Deps: []string{"f"}},
		{ID: "b", Title: "b", Status: "backlog", Deps: []string{"a"}},
		{ID: "c", Title: "c", Status: "backlog", Deps: []string{"f", "b"}},
	})
	l := buildEgo(board.NewGraph(b), "f", 2, graphHardCols, nil)
	if len(l.Skipped) != 0 {
		t.Fatalf("skipped = %v; an acyclic board has no edge the drawing cannot express", l.Skipped)
	}
	if l.DownCount != 3 {
		t.Errorf("down = %d, want a, b and c all within two hops", l.DownCount)
	}
	for id, want := range map[string]int{"a": 1, "b": 2, "c": 3} {
		if n := l.Nodes[id]; n == nil || n.Layer != want {
			t.Errorf("%s layer = %+v, want %d (the longest path, past the radius)", id, n, want)
		}
	}
	// Every induced edge is drawn, b → c included, as a forward step.
	drawn := 0
	for _, e := range l.Edges {
		if e.From == "b" && e.To == "c" {
			drawn++
		}
	}
	if drawn != 1 {
		t.Errorf("b → c drawn %d times, want once", drawn)
	}
}

// A real cycle is still the one thing the layers cannot hold, and the header
// still says so — the word "cyclic" is now only ever earned.
func TestEgoSkipsOnlyARealCycle(t *testing.T) {
	b := board.NewBoard([]*board.Task{
		{ID: "f", Title: "f", Status: "backlog"},
		{ID: "x", Title: "x", Status: "backlog", Deps: []string{"f", "y"}},
		{ID: "y", Title: "y", Status: "backlog", Deps: []string{"x"}},
	})
	l := buildEgo(board.NewGraph(b), "f", 3, graphHardCols, nil)
	if len(l.Skipped) != 1 {
		t.Fatalf("skipped = %v, want exactly the one edge the x ⇄ y cycle folds", l.Skipped)
	}
}

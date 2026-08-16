package board

import (
	"strings"
	"testing"
)

func mk(id, lane string, deps ...string) *Task {
	return &Task{ID: id, Title: id, Status: lane, Deps: deps}
}

func TestBlockedByCountsOnlyUnfinishedDeps(t *testing.T) {
	b := NewBoard([]*Task{
		mk("a", "ready", "b", "c"),
		mk("b", "done"),
		mk("c", "backlog"),
	})
	g := NewGraph(b)

	got := g.BlockedBy("a")
	if len(got) != 1 || got[0] != "c" {
		t.Errorf("BlockedBy(a) = %v, want [c] (b is done)", got)
	}
	if g.Actionable("a") {
		t.Error("a is in a next lane but has an open dep — not actionable")
	}
	if g.Actionable("c") {
		t.Error("c is in backlog, not a next lane — must not be actionable")
	}
}

func TestUnknownDepBlocks(t *testing.T) {
	b := NewBoard([]*Task{mk("a", "ready", "ghost")})
	g := NewGraph(b)
	if got := g.BlockedBy("a"); len(got) != 1 || got[0] != "ghost" {
		t.Errorf("an unknown dep must block (we cannot prove it is satisfied), got %v", got)
	}
	if g.Known("ghost") {
		t.Error("ghost is not on the board")
	}
	if g.Actionable("a") {
		t.Error("a depends on something unknown; it cannot be actionable")
	}
}

func TestReverseDeps(t *testing.T) {
	b := NewBoard([]*Task{
		mk("root", "backlog"),
		mk("x", "ready", "root"),
		mk("y", "backlog", "root"),
		mk("z", "done", "root"),
	})
	g := NewGraph(b)

	if got := g.Blocks("root"); strings.Join(got, ",") != "x,y,z" {
		t.Errorf("Blocks(root) = %v, want x,y,z", got)
	}
	// OpenBlocks is what closing root would actually free up.
	if got := g.OpenBlocks("root"); strings.Join(got, ",") != "x,y" {
		t.Errorf("OpenBlocks(root) = %v, want x,y", got)
	}
	if got := g.Blocks("x"); len(got) != 0 {
		t.Errorf("Blocks(x) = %v, want none", got)
	}
}

func TestTreeOfIsCycleSafeAndElidesDone(t *testing.T) {
	// A deliberate cycle: a -> b -> a. The real board has none, but a tree
	// walker that trusts that is a walker that hangs on the first bad merge.
	b := NewBoard([]*Task{
		mk("a", "backlog", "b"),
		mk("b", "backlog", "a"),
	})
	g := NewGraph(b)
	n := g.TreeOf("a", DirBlockedBy, 10)
	if depth(n) > 11 {
		t.Fatalf("cycle was not cut: depth %d", depth(n))
	}

	// A done blocker's own blockers are history, not context.
	b2 := NewBoard([]*Task{
		mk("top", "ready", "mid"),
		mk("mid", "done", "bottom"),
		mk("bottom", "backlog"),
	})
	n2 := NewGraph(b2).TreeOf("top", DirBlockedBy, 5)
	if len(n2.Children) != 1 {
		t.Fatalf("want one child, got %d", len(n2.Children))
	}
	kid := n2.Children[0]
	if !kid.Elided || len(kid.Children) != 0 {
		t.Errorf("a done subtree must be elided, got elided=%v children=%d", kid.Elided, len(kid.Children))
	}
}

func TestTreeOfDuplicatesSharedNodesRatherThanMerging(t *testing.T) {
	// lipgloss/v2's tree has no multi-parent support, so a DAG node reachable
	// twice is DRAWN twice and flagged, never silently merged.
	b := NewBoard([]*Task{
		mk("root", "backlog", "l", "r"),
		mk("l", "backlog", "shared"),
		mk("r", "backlog", "shared"),
		mk("shared", "backlog"),
	})
	n := NewGraph(b).TreeOf("root", DirBlockedBy, 5)
	var repeats int
	var walk func(*DepNode)
	walk = func(x *DepNode) {
		if x.Repeat {
			repeats++
		}
		for _, c := range x.Children {
			walk(c)
		}
	}
	walk(n)
	if repeats != 1 {
		t.Errorf("the second sighting of a shared node must be flagged Repeat, got %d", repeats)
	}
}

func depth(n *DepNode) int {
	d := 0
	for _, c := range n.Children {
		if x := depth(c); x > d {
			d = x
		}
	}
	return d + 1
}

// A node first reached AT the depth cap used to be recorded as "drawn" even
// though it was emitted as a bare childless leaf. When the walk later reached
// it by a SHORTER path it was flagged Repeat and not expanded, so the reader
// was told "you already saw this subtree above" while the earlier drawing had
// shown nothing — and everything under it, well inside the cap, disappeared.
func TestTreeOfExpandsANodeFirstSeenAtTheDepthCap(t *testing.T) {
	// root -> deep1 -> deep2 -> shared   (shared first met at depth 3)
	// root -> shared -> leaf             (and again at depth 1, where it fits)
	tasks := []*Task{
		{ID: "root", Status: "backlog", Deps: []string{"deep1", "shared"}},
		{ID: "deep1", Status: "backlog", Deps: []string{"deep2"}},
		{ID: "deep2", Status: "backlog", Deps: []string{"shared"}},
		{ID: "shared", Status: "backlog", Deps: []string{"leaf"}},
		{ID: "leaf", Status: "backlog"},
	}
	g := NewGraph(NewBoard(tasks))

	// A cap of 3 puts the deep sighting of `shared` exactly AT the cap.
	tree := g.TreeOf("root", DirBlockedBy, 3)

	var shallow *DepNode
	for _, c := range tree.Children {
		if c.ID == "shared" {
			shallow = c
		}
	}
	if shallow == nil {
		t.Fatal("`shared` is a direct dep of root and must appear as a child of it")
	}
	if shallow.Repeat {
		t.Error("the depth-1 sighting of `shared` is marked ↩seen, but the only earlier sighting was truncated at the cap and drew nothing")
	}
	if len(shallow.Children) == 0 {
		t.Error("`leaf` sits at depth 2, well inside the cap of 3, and was dropped from the tree entirely")
	}
}

// The Repeat marker must still fire for a genuine second sighting of a subtree
// that WAS expanded — that is the DAG-flattening contract.
func TestTreeOfStillMarksAGenuinelyRepeatedSubtree(t *testing.T) {
	tasks := []*Task{
		{ID: "root", Status: "backlog", Deps: []string{"a", "b"}},
		{ID: "a", Status: "backlog", Deps: []string{"shared"}},
		{ID: "b", Status: "backlog", Deps: []string{"shared"}},
		{ID: "shared", Status: "backlog", Deps: []string{"leaf"}},
		{ID: "leaf", Status: "backlog"},
	}
	g := NewGraph(NewBoard(tasks))
	tree := g.TreeOf("root", DirBlockedBy, 9)

	var sightings []*DepNode
	var walk func(*DepNode)
	walk = func(n *DepNode) {
		if n.ID == "shared" {
			sightings = append(sightings, n)
		}
		for _, c := range n.Children {
			walk(c)
		}
	}
	walk(tree)

	if len(sightings) != 2 {
		t.Fatalf("`shared` is reachable by two paths; got %d sightings", len(sightings))
	}
	expanded, repeats := 0, 0
	for _, s := range sightings {
		if s.Repeat {
			repeats++
		}
		if len(s.Children) > 0 {
			expanded++
		}
	}
	if expanded != 1 || repeats != 1 {
		t.Errorf("want exactly one expanded sighting and one ↩seen; got expanded=%d repeat=%d", expanded, repeats)
	}
}

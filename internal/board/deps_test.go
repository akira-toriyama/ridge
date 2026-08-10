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
	// c has no deps at all, so only its lane can disqualify it — and backlog
	// is not a next lane. (This assertion used to read
	// `!g.Actionable("c") == false && !g.Actionable("c")`, which is X && !X:
	// unsatisfiable, guarding nothing but a t.Log.)
	if g.Actionable("c") {
		t.Error("c is in backlog, which is not a next lane — it cannot be actionable")
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

package main

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
	if !g.Actionable("c") == false && !g.Actionable("c") {
		// c is in backlog, which is not a next lane.
		t.Log("c is not actionable because backlog is not a next lane")
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

func TestGraphAgreesWithFixtureFacts(t *testing.T) {
	b, g := fixtureGraph(t)

	// e-fw2m is the fixture's one epic ENTITY — no lane, no card — and the 18
	// member tasks point at it. Done/Total mirror `furrow epic ls`, so they
	// must agree with the member lanes the fixture actually holds.
	e := b.Epic("e-fw2m")
	if e == nil {
		t.Fatal("e-fw2m is the fixture's epic entity")
	}
	var members, membersDone int
	for _, task := range b.Tasks() {
		if task.Epic != e.ID {
			continue
		}
		members++
		if g.IsDone(task.ID) {
			membersDone++
		}
	}
	if members != e.Total || membersDone != e.Done {
		t.Errorf("epic reports %d/%d, members measure %d/%d", e.Done, e.Total, membersDone, members)
	}
	if e.Total != 18 {
		t.Errorf("e-fw2m has %d members, want 18", e.Total)
	}

	// t-jv3j depends on t-ehk7 (open) and t-t38k (done).
	got := g.BlockedBy("t-jv3j")
	if len(got) != 1 || got[0] != "t-ehk7" {
		t.Errorf("BlockedBy(t-jv3j) = %v, want [t-ehk7]", got)
	}

	// No dangling deps and no cycles in the fixture — the same facts measured
	// over the real 658-task board.
	for _, task := range b.Tasks() {
		for _, d := range task.Deps {
			if !g.Known(d) {
				t.Errorf("%s has a dangling dep %s", task.ID, d)
			}
		}
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
	n := g.TreeOf("a", dirBlockedBy, 10)
	if depth(n) > 11 {
		t.Fatalf("cycle was not cut: depth %d", depth(n))
	}

	// A done blocker's own blockers are history, not context.
	b2 := NewBoard([]*Task{
		mk("top", "ready", "mid"),
		mk("mid", "done", "bottom"),
		mk("bottom", "backlog"),
	})
	n2 := NewGraph(b2).TreeOf("top", dirBlockedBy, 5)
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
	n := NewGraph(b).TreeOf("root", dirBlockedBy, 5)
	var repeats int
	var walk func(*depNode)
	walk = func(x *depNode) {
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

func depth(n *depNode) int {
	d := 0
	for _, c := range n.Children {
		if x := depth(c); x > d {
			d = x
		}
	}
	return d + 1
}

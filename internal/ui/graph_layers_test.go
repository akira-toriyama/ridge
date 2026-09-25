package ui

import (
	"fmt"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The radius bounds what the ego graph includes, not how it layers what it
// included. With the layers capped at the radius, a node reachable in one hop
// but also at the end of a three-hop path sat on the outermost layer beside
// its own dependency, and the edge between them was reported as cyclic — on
// ridge-test (100 tasks, no cycle) 50 of the 100 foci at radius 2 reported a
// skipped edge, 24 of them exactly one (t-z9nf).
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
// still says so. A guard against over-correction: it passes on the capped
// layering too, and must keep passing.
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

// The layering's limit is the included set's size, not radius+1: four
// tasks one hop from the focus that also chain a1 → a2 → a3 → a4 are all
// included at radius 2, and a4 needs layer 4 — one more than a limit of
// radius+1 hands out. That limit left a4 on its one-hop layer 1 under a3 at
// layer 3, so a3 → a4 ran backwards and was skipped, and the four-task
// test above still passed.
func TestEgoLayersReachPastRadiusPlusOne(t *testing.T) {
	b := board.NewBoard([]*board.Task{
		{ID: "f", Title: "f", Status: "backlog"},
		{ID: "a1", Title: "x", Status: "backlog", Deps: []string{"f"}},
		{ID: "a2", Title: "x", Status: "backlog", Deps: []string{"f", "a1"}},
		{ID: "a3", Title: "x", Status: "backlog", Deps: []string{"f", "a2"}},
		{ID: "a4", Title: "x", Status: "backlog", Deps: []string{"f", "a3"}},
	})
	l := buildEgo(board.NewGraph(b), "f", 2, graphHardCols, nil)
	if l.DownCount != 4 {
		t.Fatalf("down = %d, want all four included at radius 2", l.DownCount)
	}
	if len(l.Skipped) != 0 {
		t.Fatalf("skipped = %v on an acyclic board", l.Skipped)
	}
	if n := l.Nodes["a4"]; n == nil || n.Layer != 4 {
		t.Errorf("a4 = %+v, want layer 4", n)
	}
}

// On any acyclic board the layered drawing expresses every edge the radius
// included: nothing skipped, every included node drawn (less what the layer
// cap admits dropping), every induced edge present once (dummy chains count
// as one). 3000 boards from a fixed xorshift stream, so a failure names its
// board: the capped layering trips at iter 4, a limit of radius+1 at
// iter 37.
func TestEgoDrawsEveryEdgeOfAnAcyclicBoard(t *testing.T) {
	seed := uint64(20260925)
	next := func(n int) int {
		seed ^= seed << 13
		seed ^= seed >> 7
		seed ^= seed << 17
		return int(seed % uint64(n)) //nolint:gosec // n is a board size or a radius: tiny and positive
	}
	for iter := 0; iter < 3000; iter++ {
		n := 3 + next(10)
		var tasks []*board.Task
		for i := 0; i < n; i++ {
			t := &board.Task{ID: fmt.Sprintf("n%02d", i), Title: "x", Status: "backlog"}
			// Edges only toward a lower index: acyclic by construction.
			for j := 0; j < i; j++ {
				if next(3) == 0 {
					t.Deps = append(t.Deps, fmt.Sprintf("n%02d", j))
				}
			}
			tasks = append(tasks, t)
		}
		b := board.NewBoard(tasks)
		g := board.NewGraph(b)
		focus := fmt.Sprintf("n%02d", next(n))
		radius := []int{1, 2, 3, graphAllRadius}[next(4)]
		l := buildEgo(g, focus, radius, graphHardCols, nil)
		if len(l.Skipped) != 0 {
			t.Fatalf("iter %d focus %s r=%d: skipped %v on an acyclic board", iter, focus, radius, l.Skipped)
		}
		// Acyclic, so no node is on both sides and the counts are distinct.
		if got := len(l.Real()) + overflowSum(l); got != l.UpCount+l.DownCount+1 {
			t.Fatalf("iter %d focus %s r=%d: %d nodes drawn or admitted dropped of %d included", iter, focus, radius, got, l.UpCount+l.DownCount+1)
		}
		// Every induced edge is drawn exactly once: follow each chain from a
		// real node through dummies to its real end.
		succ := map[string][]string{}
		for _, e := range l.Edges {
			succ[e.From] = append(succ[e.From], e.To)
		}
		drawn := map[[2]string]int{}
		for _, nd := range l.Real() {
			for _, to := range succ[nd.ID] {
				end := to
				for l.Nodes[end].Kind == egoDummy {
					end = succ[end][0]
				}
				drawn[[2]string{nd.ID, end}]++
			}
		}
		for _, nd := range l.Real() {
			for _, dep := range b.Task(nd.ID).Deps {
				if _, ok := l.Nodes[dep]; !ok {
					continue
				}
				if drawn[[2]string{dep, nd.ID}] != 1 {
					t.Fatalf("iter %d focus %s r=%d: edge %s → %s drawn %d times", iter, focus, radius, dep, nd.ID, drawn[[2]string{dep, nd.ID}])
				}
			}
		}
	}
}

// A cycle the focus is not on saturates the layering, and a node reachable
// only through a saturated node got no layer at all: it was included, counted
// in the header, and not drawn (found in review). The included set is
// backfilled, so it draws — on the outermost layer, its folded edges in
// Skipped where the header admits them.
func TestEgoDrawsWhatTheRadiusIncludedOnACyclicBoard(t *testing.T) {
	b := board.NewBoard([]*board.Task{
		{ID: "n00", Title: "x", Status: "backlog", Deps: []string{"n00", "n01"}},
		{ID: "n01", Title: "x", Status: "backlog", Deps: []string{"n06"}},
		{ID: "n02", Title: "x", Status: "backlog", Deps: []string{"n00"}},
		{ID: "n03", Title: "x", Status: "backlog", Deps: []string{"n04"}},
		{ID: "n04", Title: "x", Status: "backlog", Deps: []string{"n03"}},
		{ID: "n05", Title: "x", Status: "backlog", Deps: []string{"n00", "n01", "n04", "n06"}},
		{ID: "n06", Title: "x", Status: "backlog", Deps: []string{"n00", "n01", "n03"}},
	})
	l := buildEgo(board.NewGraph(b), "n02", 8, graphHardCols, nil)
	if got := len(l.Real()) + overflowSum(l); got != l.UpCount+l.DownCount+1 {
		t.Fatalf("%d nodes drawn or admitted dropped of %d included (up %d / down %d)", got, l.UpCount+l.DownCount+1, l.UpCount, l.DownCount)
	}
	if l.Nodes["n04"] == nil {
		t.Fatal("n04 was included by the radius and is not drawn")
	}
}

// overflowSum is how many included nodes the layer cap admits dropping.
func overflowSum(l *egoLayout) int {
	n := 0
	for _, v := range l.Overflow {
		n += v
	}
	return n
}

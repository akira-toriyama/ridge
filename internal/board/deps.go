package board

import "sort"

// Graph is the ONE place "blocked", "actionable" and "reverse deps" are
// defined. The card glyph, the filter, the detail pane and the dep-tree
// overlay all read it, so they cannot drift apart.
//
// It is rebuilt after every mutation — with 24 tasks (and 658 on the real
// board, 102 edges) that is free, and it removes a whole class of stale-cache
// bugs.
type Graph struct {
	b   *Board
	rev map[string][]string // id -> ids that depend on it
}

// NewGraph indexes the reverse dep edges of a board.
func NewGraph(b *Board) *Graph {
	g := &Graph{b: b, rev: map[string][]string{}}
	for _, t := range b.Tasks() {
		for _, d := range t.Deps {
			g.rev[d] = append(g.rev[d], t.ID)
		}
	}
	for k := range g.rev {
		sort.Strings(g.rev[k])
	}
	return g
}

// Board is the board this graph indexes.
func (g *Graph) Board() *Board { return g.b }

// Known reports whether the id names a task on the board. A dep pointing at an
// absent id renders "?" rather than silently counting as satisfied.
func (g *Graph) Known(id string) bool { return g.b.Task(id) != nil }

// IsDone reports whether the task exists and sits in a done lane.
func (g *Graph) IsDone(id string) bool {
	t := g.b.Task(id)
	return t != nil && g.b.isDoneLane(t.Status)
}

// BlockedBy lists the task's deps that are NOT done, in dep order. An unknown
// dep counts as blocking: we cannot prove it is satisfied.
func (g *Graph) BlockedBy(id string) []string {
	t := g.b.Task(id)
	if t == nil {
		return nil
	}
	var out []string
	for _, d := range t.Deps {
		if !g.IsDone(d) {
			out = append(out, d)
		}
	}
	return out
}

// Blocks lists the tasks that depend on this one — the reverse edges, which
// exist nowhere on disk and are the half of a dep a raw shard cannot show you.
func (g *Graph) Blocks(id string) []string { return g.rev[id] }

// OpenBlocks is Blocks restricted to tasks that are not done, i.e. what would
// actually be unblocked by closing this task.
func (g *Graph) OpenBlocks(id string) []string {
	var out []string
	for _, x := range g.rev[id] {
		if !g.IsDone(x) {
			out = append(out, x)
		}
	}
	return out
}

// Actionable is exactly what `furrow next` would hand you: in a next lane and
// every dep done.
func (g *Graph) Actionable(id string) bool {
	t := g.b.Task(id)
	if t == nil {
		return false
	}
	l := g.b.Lane(t.Status)
	if l == nil || !l.Next {
		return false
	}
	return len(g.BlockedBy(id)) == 0
}

// Dir selects which way TreeOf walks.
type Dir int

const (
	// DirBlockedBy walks "what must finish before this" (task -> its deps).
	DirBlockedBy Dir = iota
	// DirBlocks walks "what this unblocks" (task -> its reverse deps).
	DirBlocks
)

// DepNode is one node of a rendered dep tree.
type DepNode struct {
	ID       string
	Elided   bool // a done subtree, collapsed
	Repeat   bool // already drawn elsewhere in this tree (shared node in a DAG)
	Children []*DepNode
}

// TreeOf builds a depth-capped, cycle-safe dependency tree in one direction.
//
// It is a TREE over a DAG: a node reachable by two paths is drawn twice (marked
// Repeat) rather than merged, because lipgloss/v2's tree has no multi-parent
// support. Measured over the real board that costs ~2.5% extra rows — far
// cheaper than a Sugiyama layout engine, which no Go library provides and which
// a median live component of 2 nodes does not justify.
//
// Done subtrees are elided: once a blocker is closed, its own blockers are
// history, not context.
func (g *Graph) TreeOf(id string, dir Dir, maxDepth int) *DepNode {
	drawn := map[string]bool{}
	var build func(string, int, map[string]bool) *DepNode
	build = func(cur string, depth int, path map[string]bool) *DepNode {
		n := &DepNode{ID: cur}
		if drawn[cur] && depth > 0 {
			n.Repeat = true
			return n
		}
		if depth >= maxDepth || path[cur] {
			return n
		}
		if depth > 0 && g.IsDone(cur) {
			n.Elided = true
			return n
		}
		// Marked drawn only once the node is actually EXPANDED. Setting it
		// above the two returns meant a node first reached AT the cap was
		// emitted as a bare childless leaf and still counted as drawn — so a
		// later, shallower sighting rendered `↩seen` ("you saw this subtree
		// above") when the earlier drawing had shown nothing, and the in-cap
		// remainder of that subtree vanished from the tree. Termination is
		// unaffected: drawn still stops re-expansion, path[] still cuts
		// cycles, and depth still only grows.
		drawn[cur] = true
		var next []string
		if dir == DirBlockedBy {
			if t := g.b.Task(cur); t != nil {
				next = t.Deps
			}
		} else {
			next = g.rev[cur]
		}
		sub := map[string]bool{cur: true}
		for k := range path {
			sub[k] = true
		}
		for _, c := range next {
			n.Children = append(n.Children, build(c, depth+1, sub))
		}
		return n
	}
	return build(id, 0, map[string]bool{})
}

package board

import "sort"

// Dependency CLUSTERS: the whole board's dep edges grouped into connected
// components, each member carrying the depth and the blocker names an indented
// overview needs. This is the "all of it at once" counterpart to TreeOf, which
// answers about one task.
//
// It lives here rather than in the renderer for the same reason BlockedBy does:
// "which tasks are one tangle" is a fact about the board, not about a frame.
// It is NOT delegated to furrow because furrow has no command of this shape —
// `furrow dep --list` and the `depends-on:`/`ancestor-of:` query qualifiers are
// all rooted on ONE id, and no JSON shape carries a component or a depth. So
// this is the same deal Blocks already struck: pure topology over the forward
// edges furrow does hand back, adding no judgment of its own about what a dep,
// "done" or "blocked" means.

// ClusterScope selects which tasks take part in the grouping.
type ClusterScope int

const (
	// ClusterOpen drops every done task AND every edge touching one, which is
	// the useful default: a finished dep is not a blocker, so leaving it in
	// welds live clusters onto dead ones and buries what is actually in the
	// way. Measured on the real board, all-scope collapsed 32 components into
	// a handful of giants; open-scope leaves 9 small live ones.
	ClusterOpen ClusterScope = iota
	// ClusterAll keeps everything, including components that are entirely done
	// — the history of a tangle, and the only way to see a done task's place
	// in one.
	ClusterAll
)

func (s ClusterScope) String() string {
	if s == ClusterAll {
		return "all"
	}
	return "open"
}

// ClusterNode is one member of a cluster.
type ClusterNode struct {
	ID string
	// Depth is the longest chain of in-scope blockers ABOVE this node, in
	// edges. 0 = nothing in scope blocks it. It is the indent of an overview
	// row, so it is longest-path (not shortest): a node must never be drawn
	// above something it waits on.
	Depth int
	// Blockers are this node's in-scope deps, deduplicated and sorted. Unknown
	// ids are INCLUDED — they block (nothing proves them satisfied) and naming
	// them is how a dangling dep gets noticed — but they are not cluster
	// members, so they do not join two tasks into one component.
	Blockers []string
	// Open is the subset of Blockers that is NOT satisfied — Graph.BlockedBy
	// restricted to this cluster's scope.
	//
	// It exists because at ClusterAll the two differ, and a count taken from
	// Blockers there contradicts the board's own `x` marker on the very same
	// row. Blockers answers "what edges does this node have" (the `←` tag);
	// Open answers "is it stuck" (every count).
	Open []string
	// Blocking is how many OPEN members this node blocks, directly or
	// transitively — what closing it would actually free. The whole point of an
	// overview: "these eight are held up by exactly two tasks" is a fact no
	// per-task view can state. Done members are not counted: they are not
	// waiting on anything.
	Blocking int
	// Done reports a member in a done lane. Only reachable at ClusterAll.
	Done bool
}

// Cluster is one connected component of the dependency graph, in draw order.
type Cluster struct {
	// Nodes are ordered by (Depth, ID) — the reading order of an indented
	// list, and deterministic, so the same board always draws the same shape.
	// A dependency DAG has no time axis to fall back on, so an unstable order
	// would redraw the same tangle differently on every frame.
	Nodes []ClusterNode
}

// Depth is the cluster's longest blocker chain, in edges.
func (c Cluster) Depth() int {
	d := 0
	for _, n := range c.Nodes {
		if n.Depth > d {
			d = n.Depth
		}
	}
	return d
}

// Roots counts the members where work can start: unfinished, and waiting on
// nothing. Blocked() is its complement among the unfinished, and Done() the
// rest — the three add up to len(Nodes), and each one is readable off the row
// markers, which is the point. Counting a done task as a root put "7
// unblocked" in green over seven rows the same frame marked `v`.
func (c Cluster) Roots() int {
	n := 0
	for _, nd := range c.Nodes {
		if !nd.Done && len(nd.Open) == 0 {
			n++
		}
	}
	return n
}

// Blocked counts the members with at least one unsatisfied dep — exactly the
// rows the board's own marker draws as blocked.
func (c Cluster) Blocked() int {
	n := 0
	for _, nd := range c.Nodes {
		if len(nd.Open) > 0 {
			n++
		}
	}
	return n
}

// Done counts the finished members. Always 0 at ClusterOpen.
func (c Cluster) Done() int {
	n := 0
	for _, nd := range c.Nodes {
		if nd.Done {
			n++
		}
	}
	return n
}

// Top is the OPEN member whose closing would free the most others, or the zero
// value when nothing in the cluster frees anything. Ties go to the first in
// draw order, so the answer is stable across frames. Done members are excluded
// as subjects: naming a finished task as the one to close is advice nobody can
// take.
func (c Cluster) Top() ClusterNode {
	var best ClusterNode
	for _, n := range c.Nodes {
		if !n.Done && n.Blocking > best.Blocking {
			best = n
		}
	}
	return best
}

// Clusters groups the board's dependency edges into connected components.
//
// A component is kept when it has at least one edge — so a task with a single
// dangling dep and nothing else is a one-member cluster (its `←` names an id
// that is not on the board, which is a defect worth surfacing), while the
// board's many edgeless tasks are simply absent. Order is by size, then depth,
// then first id: biggest tangle first, and stable.
func (g *Graph) Clusters(scope ClusterScope) []Cluster {
	inScope := func(id string) bool {
		t := g.b.Task(id)
		return t != nil && (scope == ClusterAll || !g.b.isDoneLane(t.Status))
	}

	// blockers[id] = the in-scope deps of id; fwd[id] = the members id blocks.
	// Built over SORTED tasks so every map iteration below can be replaced by
	// a walk of a sorted slice — determinism is a hard requirement here.
	blockers := map[string][]string{}
	fwd := map[string][]string{}
	var members []string
	seen := map[string]bool{}
	join := func(id string) {
		if !seen[id] {
			seen[id] = true
			members = append(members, id)
		}
	}

	tasks := g.b.Tasks()
	ids := make([]string, 0, len(tasks))
	for _, t := range tasks {
		ids = append(ids, t.ID)
	}
	sort.Strings(ids)

	for _, id := range ids {
		if !inScope(id) {
			continue
		}
		for _, d := range g.b.Task(id).Deps {
			switch {
			case !g.Known(d):
				// A dangling dep. It blocks and it is named, but a phantom
				// cannot be a member, so it welds nothing together.
				blockers[id] = append(blockers[id], d)
				join(id)
			case inScope(d):
				blockers[id] = append(blockers[id], d)
				fwd[d] = append(fwd[d], id)
				join(id)
				join(d)
			}
		}
	}
	// Deduplicate: a task may legitimately list the same dep twice, and the
	// map's `←` tag is a list of NAMES — "←t-a,t-a" reads as two edges and
	// spends the whole tag budget saying one thing.
	for id := range blockers {
		blockers[id] = dedupe(blockers[id])
	}
	for id := range fwd {
		fwd[id] = dedupe(fwd[id])
	}

	comps := components(members, seen, blockers, fwd)

	out := make([]Cluster, 0, len(comps))
	for _, comp := range comps {
		out = append(out, g.buildCluster(comp, blockers, fwd))
	}
	sort.SliceStable(out, func(a, b int) bool {
		if len(out[a].Nodes) != len(out[b].Nodes) {
			return len(out[a].Nodes) > len(out[b].Nodes)
		}
		if da, db := out[a].Depth(), out[b].Depth(); da != db {
			return da > db
		}
		return out[a].Nodes[0].ID < out[b].Nodes[0].ID
	})
	return out
}

// components walks the edges as UNDIRECTED — being in one tangle is not a
// direction — and returns each component's members in sorted order. isMember
// is what keeps a dangling dep out: it is named as a blocker but is not on the
// board, so it must not weld two tasks that merely share it.
func components(members []string, isMember map[string]bool, blockers, fwd map[string][]string) [][]string {
	sorted := append([]string(nil), members...)
	sort.Strings(sorted)

	adj := func(id string) []string {
		var out []string
		for _, b := range blockers[id] {
			if isMember[b] {
				out = append(out, b)
			}
		}
		return append(out, fwd[id]...)
	}

	visited := map[string]bool{}
	var comps [][]string
	for _, root := range sorted {
		if visited[root] {
			continue
		}
		// An explicit stack, not recursion: a pathological board should not be
		// able to blow the render goroutine's stack.
		visited[root] = true
		comp := []string{root}
		stack := []string{root}
		for len(stack) > 0 {
			id := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			for _, nb := range adj(id) {
				if visited[nb] {
					continue
				}
				visited[nb] = true
				comp = append(comp, nb)
				stack = append(stack, nb)
			}
		}
		sort.Strings(comp)
		comps = append(comps, comp)
	}
	return comps
}

// dedupe sorts and removes repeats in place.
func dedupe(ids []string) []string {
	if len(ids) < 2 {
		return ids
	}
	sort.Strings(ids)
	out := ids[:1]
	for _, id := range ids[1:] {
		if id != out[len(out)-1] {
			out = append(out, id)
		}
	}
	return out
}

// buildCluster layers one component and counts what each member holds up.
func (g *Graph) buildCluster(comp []string, blockers, fwd map[string][]string) Cluster {
	within := map[string]bool{}
	for _, id := range comp {
		within[id] = true
	}

	// Longest-path depth by bounded relaxation, NOT by DFS: the real board has
	// no cycles, but a merge can produce one and a view that hangs is worse
	// than one that draws a tangle awkwardly.
	//
	// The cap is what makes a cycle behave. A DAG's longest chain is at most
	// len(comp)-1 edges, so clamping there changes NOTHING for real input, and
	// a cycle saturates at that bound instead of climbing one per round —
	// which would put "depth 5" in the header of a three-node tangle.
	maxDepth := len(comp) - 1
	depth := map[string]int{}
	for round := 0; round < len(comp); round++ {
		changed := false
		for _, u := range comp {
			for _, v := range fwd[u] {
				if !within[v] {
					continue
				}
				d := depth[u] + 1
				if d > maxDepth {
					d = maxDepth
				}
				if d > depth[v] {
					depth[v] = d
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}

	nodes := make([]ClusterNode, 0, len(comp))
	for _, id := range comp {
		var open []string
		for _, b := range blockers[id] {
			if !g.IsDone(b) {
				open = append(open, b)
			}
		}
		nodes = append(nodes, ClusterNode{
			ID:       id,
			Depth:    depth[id],
			Blockers: blockers[id],
			Open:     open,
			Blocking: g.reach(id, fwd, within),
			Done:     g.IsDone(id),
		})
	}
	sort.SliceStable(nodes, func(a, b int) bool {
		if nodes[a].Depth != nodes[b].Depth {
			return nodes[a].Depth < nodes[b].Depth
		}
		return nodes[a].ID < nodes[b].ID
	})
	return Cluster{Nodes: nodes}
}

// reach counts the OPEN members downstream of id — the transitive blast radius
// of closing it. Cycle-safe by the visited set, which also makes a node
// reachable by two paths count ONCE, so the number is "how many tasks this
// frees" and not "how many paths lead away from it". Done members are walked
// THROUGH but not counted: closing a task does not free something already
// finished, and saying it did is how "frees 10" ended up over ten `v` rows.
func (g *Graph) reach(id string, fwd map[string][]string, within map[string]bool) int {
	visited := map[string]bool{id: true}
	stack := []string{id}
	n := 0
	for len(stack) > 0 {
		u := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, v := range fwd[u] {
			if !within[v] || visited[v] {
				continue
			}
			visited[v] = true
			if !g.IsDone(v) {
				n++
			}
			stack = append(stack, v)
		}
	}
	return n
}

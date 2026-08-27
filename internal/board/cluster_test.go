package board

import (
	"reflect"
	"strings"
	"testing"
)

// ids is the members of one cluster, joined for a legible failure message.
func ids(c Cluster) []string {
	out := make([]string, 0, len(c.Nodes))
	for _, n := range c.Nodes {
		out = append(out, n.ID)
	}
	return out
}

func node(t *testing.T, c Cluster, id string) ClusterNode {
	t.Helper()
	for _, n := range c.Nodes {
		if n.ID == id {
			return n
		}
	}
	t.Fatalf("%s is not in the cluster %v", id, ids(c))
	return ClusterNode{}
}

// The whole point of the default scope: a finished dep is not a blocker, so
// leaving it in welds every live cluster onto the dead spine that already
// closed. Measured on this fixture-shaped board: all-scope is ONE tangle of
// five, open-scope is two separate live pairs.
func TestOpenScopeDropsDoneTasksAndTheEdgesThroughThem(t *testing.T) {
	b := NewBoard([]*Task{
		mk("spine", "done"),
		mk("a", "backlog", "spine"),
		mk("b", "backlog", "a"),
		mk("c", "backlog", "spine"),
		mk("lonely", "backlog", "spine"), // its ONLY edge runs through the done task
	})
	g := NewGraph(b)

	all := g.Clusters(ClusterAll)
	if len(all) != 1 {
		t.Fatalf("scope=all groups everything through the done spine, got %d clusters: %v", len(all), all)
	}
	if got := ids(all[0]); len(got) != 5 {
		t.Errorf("scope=all members = %v, want all five", got)
	}

	open := g.Clusters(ClusterOpen)
	if len(open) != 1 {
		t.Fatalf("scope=open leaves only the a→b chain, got %d clusters: %v", len(open), open)
	}
	if got, want := ids(open[0]), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Errorf("scope=open members = %v, want %v", got, want)
	}
	// c and lonely each lost their only edge with the done task; a node with no
	// edges left is not a cluster of one, it is simply absent.
	for _, id := range []string{"c", "lonely", "spine"} {
		for _, c := range open {
			for _, n := range c.Nodes {
				if n.ID == id {
					t.Errorf("%s has no open edge but still appears in %v", id, ids(c))
				}
			}
		}
	}
}

// Depth is LONGEST path, not shortest. Getting this backwards draws a node
// above something it waits on, which is the one thing the indent must never do.
func TestDepthIsTheLongestChainNotTheShortest(t *testing.T) {
	// far reaches sink by three edges, near by one. Sink must sit at 3.
	b := NewBoard([]*Task{
		mk("near", "backlog"),
		mk("far", "backlog"),
		mk("mid1", "backlog", "far"),
		mk("mid2", "backlog", "mid1"),
		mk("sink", "backlog", "near", "mid2"),
	})
	c := NewGraph(b).Clusters(ClusterAll)
	if len(c) != 1 {
		t.Fatalf("want one cluster, got %d", len(c))
	}
	if got := node(t, c[0], "sink").Depth; got != 3 {
		t.Errorf("sink depth = %d, want 3 (the long way round, not the short one)", got)
	}
	if got := c[0].Depth(); got != 3 {
		t.Errorf("cluster depth = %d, want 3", got)
	}
	// And the draw order really is (depth, id): a reader scanning down must
	// never meet a node before its blocker.
	seen := map[string]bool{}
	for _, n := range c[0].Nodes {
		for _, blk := range n.Blockers {
			if !seen[blk] {
				t.Errorf("%s is drawn before its blocker %s", n.ID, blk)
			}
		}
		seen[n.ID] = true
	}
}

// A dep pointing at an id that is not on the board blocks and must be NAMED —
// but it is a phantom, so it cannot weld two unrelated tasks into one tangle.
func TestDanglingDepIsNamedButGroupsNothing(t *testing.T) {
	b := NewBoard([]*Task{
		mk("a", "backlog", "ghost"),
		mk("b", "backlog", "ghost"),
	})
	cs := NewGraph(b).Clusters(ClusterOpen)
	if len(cs) != 2 {
		t.Fatalf("two tasks sharing a PHANTOM are not one cluster, got %d: %v", len(cs), cs)
	}
	for _, c := range cs {
		if len(c.Nodes) != 1 {
			t.Fatalf("cluster %v should hold one member", ids(c))
		}
		if got := c.Nodes[0].Blockers; len(got) != 1 || got[0] != "ghost" {
			t.Errorf("%s blockers = %v, want [ghost] — an unresolved dep must still be named",
				c.Nodes[0].ID, got)
		}
		if c.Roots() != 0 {
			t.Errorf("%s is blocked by a phantom; it is not a root", c.Nodes[0].ID)
		}
	}
}

// Blocking is "how many tasks closing this frees", so a node reachable by two
// paths counts ONCE. Counting paths instead of nodes inflates every diamond.
func TestBlockingCountsMembersNotPaths(t *testing.T) {
	b := NewBoard([]*Task{
		mk("top", "backlog"),
		mk("l", "backlog", "top"),
		mk("r", "backlog", "top"),
		mk("sink", "backlog", "l", "r"),
	})
	c := NewGraph(b).Clusters(ClusterAll)[0]
	if got := node(t, c, "top").Blocking; got != 3 {
		t.Errorf("top frees l, r and sink = 3, got %d (sink counted twice?)", got)
	}
	if got := c.Top(); got.ID != "top" {
		t.Errorf("Top() = %q, want top", got.ID)
	}
	// And the diamond's sink is listed exactly once, which is the map's whole
	// advantage over the tree view.
	n := 0
	for _, nd := range c.Nodes {
		if nd.ID == "sink" {
			n++
		}
	}
	if n != 1 {
		t.Errorf("sink appears %d times; a cluster lists every member once", n)
	}
}

// The real board has no cycles, but a merge can produce one, and a view that
// hangs is worse than one that draws a tangle awkwardly.
func TestACycleTerminatesAndStaysOneCluster(t *testing.T) {
	b := NewBoard([]*Task{
		mk("a", "backlog", "b"),
		mk("b", "backlog", "c"),
		mk("c", "backlog", "a"),
	})
	cs := NewGraph(b).Clusters(ClusterAll)
	if len(cs) != 1 || len(cs[0].Nodes) != 3 {
		t.Fatalf("a 3-cycle is one cluster of three, got %v", cs)
	}
	if d := cs[0].Depth(); d < 0 || d > 3 {
		t.Errorf("a saturated cycle depth = %d, want it bounded by the member count", d)
	}
	if cs[0].Roots() != 0 {
		t.Error("every node in a cycle is blocked; none is a root")
	}
}

// A dependency DAG has no time axis to fall back on, so an unstable order would
// redraw the same tangle differently on every frame.
func TestClusterOrderIsStableAndBiggestFirst(t *testing.T) {
	b := NewBoard([]*Task{
		mk("s1", "backlog"), mk("s2", "backlog", "s1"),
		mk("b1", "backlog"), mk("b2", "backlog", "b1"),
		mk("b3", "backlog", "b2"), mk("b4", "backlog", "b3"),
	})
	g := NewGraph(b)
	first := g.Clusters(ClusterOpen)
	if len(first) != 2 {
		t.Fatalf("want two clusters, got %d", len(first))
	}
	if len(first[0].Nodes) < len(first[1].Nodes) {
		t.Errorf("clusters come biggest first, got %v then %v", ids(first[0]), ids(first[1]))
	}
	for i := 0; i < 5; i++ {
		again := g.Clusters(ClusterOpen)
		for j := range first {
			if !reflect.DeepEqual(ids(first[j]), ids(again[j])) {
				t.Fatalf("run %d reordered cluster %d: %v then %v",
					i, j, ids(first[j]), ids(again[j]))
			}
		}
	}
}

func TestClusterScopeNamesItself(t *testing.T) {
	if got := ClusterOpen.String(); got != "open" {
		t.Errorf("ClusterOpen = %q", got)
	}
	if got := ClusterAll.String(); got != "all" {
		t.Errorf("ClusterAll = %q", got)
	}
}

// An edgeless board is a healthy board, not an error: the view says so in
// words and this is where "no clusters at all" is pinned as legal.
func TestABoardWithNoEdgesHasNoClusters(t *testing.T) {
	b := NewBoard([]*Task{mk("a", "backlog"), mk("b", "ready")})
	if got := NewGraph(b).Clusters(ClusterAll); len(got) != 0 {
		t.Errorf("no dep edges means no clusters, got %v", got)
	}
}

// The blockers a node lists are the ones IN SCOPE — the tag is the map's
// substitute for drawing a line, so naming a satisfied dep as a live blocker
// would be the same lie as drawing an edge that no longer holds.
func TestBlockersAreScopedAndSorted(t *testing.T) {
	b := NewBoard([]*Task{
		mk("done1", "done"),
		mk("z", "backlog"),
		mk("a", "backlog"),
		mk("sink", "backlog", "z", "done1", "a"),
	})
	g := NewGraph(b)

	got := node(t, g.Clusters(ClusterAll)[0], "sink").Blockers
	if want := []string{"a", "done1", "z"}; !reflect.DeepEqual(got, want) {
		t.Errorf("scope=all blockers = %v, want %v (sorted, done included)", got, want)
	}
	got = node(t, g.Clusters(ClusterOpen)[0], "sink").Blockers
	if want := []string{"a", "z"}; !reflect.DeepEqual(got, want) {
		t.Errorf("scope=open blockers = %v, want %v (the done dep is not a blocker)",
			got, strings.Join(want, ","))
	}
}

// At ClusterAll the cluster still holds finished tasks, and the counts must
// stay readable off the row markers the caller draws beside them. Before this
// they were pure topology: a done task counted as a root ("where work can
// start"), a satisfied dep counted as a blocker, and Top named an already
// closed task as the one to close — three answers for the same row in one
// frame.
func TestCountsAtAllScopeAgreeWithWhatIsActuallyBlocked(t *testing.T) {
	b := NewBoard([]*Task{
		mk("root", "done"),            // finished: not a root, not blocked
		mk("mid", "backlog", "root"),  // its only dep is done -> startable
		mk("leaf", "backlog", "mid"),  // genuinely blocked
		mk("leaf2", "backlog", "mid"), // ditto
	})
	g := NewGraph(b)
	c := g.Clusters(ClusterAll)[0]

	if got := len(c.Nodes); got != 4 {
		t.Fatalf("scope=all holds %d members, want 4: %v", got, ids(c))
	}
	if got := c.Roots(); got != 1 {
		t.Errorf("Roots() = %d, want 1 (only mid can be started; root is finished)", got)
	}
	if got := c.Blocked(); got != 2 {
		t.Errorf("Blocked() = %d, want 2 (leaf and leaf2)", got)
	}
	if got := c.Done(); got != 1 {
		t.Errorf("Done() = %d, want 1", got)
	}
	if got := c.Roots() + c.Blocked() + c.Done(); got != len(c.Nodes) {
		t.Errorf("the three counts sum to %d, want %d — they must partition the panel",
			got, len(c.Nodes))
	}

	// "root" reaches three members but frees only the two that are waiting,
	// and it is finished, so it is not the task to close.
	if top := c.Top(); top.ID != "mid" {
		t.Errorf("Top() = %q, want mid — a finished task is not advice", top.ID)
	}
	if got := node(t, c, "mid").Blocking; got != 2 {
		t.Errorf("mid frees %d, want 2", got)
	}
	if got := node(t, c, "root").Blocking; got != 3 {
		t.Errorf("root's blast radius is %d open tasks, want 3", got)
	}

	// Blockers is the EDGE list (what "←" names); Open is what still blocks.
	n := node(t, c, "mid")
	if len(n.Blockers) != 1 || n.Blockers[0] != "root" {
		t.Errorf("mid blockers = %v, want [root] — the edge is still there", n.Blockers)
	}
	if len(n.Open) != 0 {
		t.Errorf("mid open blockers = %v, want none — root is done", n.Open)
	}
	if !node(t, c, "root").Done {
		t.Error("root sits in a done lane and is not marked Done")
	}
}

// The "←" tag is a list of NAMES, so a dep listed twice must not read as two
// edges — and must not spend the whole tag budget saying one thing.
func TestADuplicatedDepIsNamedOnce(t *testing.T) {
	b := NewBoard([]*Task{
		mk("a", "backlog"),
		mk("z", "backlog", "a", "a", "a"),
	})
	c := NewGraph(b).Clusters(ClusterOpen)[0]
	if got := node(t, c, "z").Blockers; len(got) != 1 || got[0] != "a" {
		t.Errorf("z blockers = %v, want [a]", got)
	}
	if got := node(t, c, "a").Blocking; got != 1 {
		t.Errorf("a frees %d, want 1 — z is one task however often it names a", got)
	}
	if got := len(c.Nodes); got != 2 {
		t.Errorf("the cluster holds %d members, want 2: %v", got, ids(c))
	}
}

// A task depending on itself must neither hang the layering nor be counted as
// freeing itself.
func TestASelfDependencyIsBlockedByItselfAndNothingElse(t *testing.T) {
	b := NewBoard([]*Task{mk("loop", "backlog", "loop")})
	cs := NewGraph(b).Clusters(ClusterOpen)
	if len(cs) != 1 || len(cs[0].Nodes) != 1 {
		t.Fatalf("want one one-member cluster, got %v", cs)
	}
	n := cs[0].Nodes[0]
	if len(n.Open) != 1 || n.Open[0] != "loop" {
		t.Errorf("open blockers = %v, want [loop]", n.Open)
	}
	if n.Blocking != 0 {
		t.Errorf("loop frees %d, want 0 — it cannot free itself", n.Blocking)
	}
	if cs[0].Roots() != 0 || cs[0].Blocked() != 1 {
		t.Errorf("roots=%d blocked=%d, want 0 and 1", cs[0].Roots(), cs[0].Blocked())
	}
}

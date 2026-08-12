package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// The graph is the one view whose point is that you WALK it — glossary calls
// re-rooting "the thing a static picture cannot do". Every handler behind that
// walk was at 0% coverage, and no -demo frame can prove any of them: a demo is
// one still, and re-root/retrace/radius are transitions between stills.
//
// Most of these call the handlers directly rather than through Update(), and
// graphModel renders once first so graphLay exists. The exception is
// TestClosingTheGraphBeforeItRendersDoesNotPanic, which deliberately skips that
// render — the state every headless driver in this package can reach and the
// one the nil guard exists for.
//
// Only three of these fail against the pre-fix source (the two panics and the
// epic-id leak). The rest are new coverage for handlers that were correct but
// untested; they are verified by mutation instead — dropping the graphStack
// push, freezing cycleGraphRadius, or removing closeGraph's cursor carry each
// turns one of them red.

// graphModel opens the graph rooted on a task that has structure in both
// directions, and renders once so graphLay exists.
func graphModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := boardModel(t, w, h)
	for _, task := range m.b.Tasks() {
		if len(m.g.BlockedBy(task.ID)) > 0 && len(m.g.Blocks(task.ID)) > 0 {
			m.selectID(task.ID, false)
			m.openGraph()
			m.View()
			return m
		}
	}
	// Fall back to anything with structure at all.
	for _, task := range m.b.Tasks() {
		if len(m.g.BlockedBy(task.ID))+len(m.g.Blocks(task.ID)) > 0 {
			m.selectID(task.ID, false)
			m.openGraph()
			m.View()
			return m
		}
	}
	t.Skip("the fixture has no task with dependencies")
	return nil
}

// S then esc with no render in between must not panic. bubbletea renders after
// every Update, so a keyboard user is shielded — but every headless driver in
// this package is not, which is why the walk could not be tested at all.
func TestClosingTheGraphBeforeItRendersDoesNotPanic(t *testing.T) {
	m := boardModel(t, 240, 60)
	m.openGraph()
	if m.view != viewGraph {
		t.Fatal("S did not open the graph")
	}
	if m.graphLay != nil {
		t.Fatal("this test is only meaningful before the first render")
	}
	m.closeGraph() // panicked here: Node() dereferences a nil *egoLayout
	if m.view != viewBoard {
		t.Errorf("esc left the view at %v", m.view)
	}
}

// Re-rooting pushes onto the walk stack and `<` retraces it. Deleting the push
// or swapping the two handlers left the whole suite green.
func TestGraphWalkRerootsAndRetraces(t *testing.T) {
	m := graphModel(t, 240, 60)
	start := m.graphFocus

	moved := false
	for _, dir := range [][2]int{{0, -1}, {0, +1}, {+1, 0}, {-1, 0}} {
		m.graphMove(dir[0], dir[1])
		if m.graphSel != start {
			moved = true
			break
		}
	}
	if !moved {
		t.Skip("no reachable neighbour to move the graph selection to")
	}
	target := m.graphSel

	m.rerootGraph()
	if m.graphFocus != target {
		t.Fatalf("re-root left the focus on %s, want %s", m.graphFocus, target)
	}
	if len(m.graphStack) != 1 || m.graphStack[0] != start {
		t.Fatalf("re-root did not push %s onto the walk stack: %v", start, m.graphStack)
	}

	m.graphBack()
	if m.graphFocus != start {
		t.Errorf("`<` retraced to %s, want %s", m.graphFocus, start)
	}
	if len(m.graphStack) != 0 {
		t.Errorf("`<` left %d entries on the walk stack", len(m.graphStack))
	}
}

// Re-rooting on the node that is already the root is a no-op, and it must not
// grow the stack — otherwise `<` burns a press going nowhere.
func TestRerootOnTheCurrentRootIsANoOp(t *testing.T) {
	m := graphModel(t, 240, 60)
	focus := m.graphFocus
	m.graphSel = focus

	m.rerootGraph()

	if m.graphFocus != focus {
		t.Errorf("focus moved to %s", m.graphFocus)
	}
	if len(m.graphStack) != 0 {
		t.Errorf("re-rooting on the current root pushed %v onto the walk stack", m.graphStack)
	}
}

// `<` at the start of the walk says so rather than doing nothing silently.
func TestGraphBackAtTheStartOfTheWalkReports(t *testing.T) {
	m := graphModel(t, 240, 60)
	m.status = ""
	m.graphBack()
	if m.status == "" {
		t.Error("`<` with an empty walk stack changed nothing and said nothing")
	}
}

// z cycles the hop radius through every configured value and wraps.
func TestGraphRadiusCyclesAndWraps(t *testing.T) {
	m := graphModel(t, 240, 60)
	if len(graphRadii) < 2 {
		t.Skip("only one radius configured")
	}
	seen := make([]int, 0, len(graphRadii))
	for range graphRadii {
		m.cycleGraphRadius()
		seen = append(seen, m.graphRadius)
	}
	for _, r := range graphRadii {
		found := false
		for _, s := range seen {
			if s == r {
				found = true
			}
		}
		if !found {
			t.Errorf("cycling never reached radius %d (saw %v)", r, seen)
		}
	}
	first := m.graphRadius
	for range graphRadii {
		m.cycleGraphRadius()
	}
	if m.graphRadius != first {
		t.Errorf("a full cycle did not wrap: %d -> %d", first, m.graphRadius)
	}
}

// Closing the graph lands the board cursor on wherever the walk ended — the
// walk was navigation, so it should have moved you.
func TestClosingTheGraphCarriesTheCursorToWhereTheWalkEnded(t *testing.T) {
	m := graphModel(t, 240, 60)
	start := m.graphFocus

	moved := false
	for _, dir := range [][2]int{{0, -1}, {0, +1}, {+1, 0}, {-1, 0}} {
		m.graphMove(dir[0], dir[1])
		if m.graphSel != start {
			moved = true
			break
		}
	}
	if !moved {
		t.Skip("no reachable neighbour to move the graph selection to")
	}
	n := m.graphLay.Node(m.graphSel)
	if n == nil || n.Kind != egoReal {
		t.Skip("the selection is not a real node")
	}
	want := n.ID

	m.closeGraph()

	if m.view != viewBoard {
		t.Fatalf("esc left the view at %v", m.view)
	}
	if got := m.curTask(); got == nil || got.ID != want {
		id := "<nil>"
		if got != nil {
			id = got.ID
		}
		t.Errorf("the board cursor landed on %s, want the node the walk ended on (%s)", id, want)
	}
}

// A task whose lane is not in the board's vocabulary is never in a column, but
// it IS drawn as a graph node. Board.Lane returns nil for it and the renderer
// dereferenced that. This state cannot be produced through -dump (the fixture
// is forced), so a unit test is the only harness.
func TestGraphRendersATaskWhoseLaneIsNotInTheVocabulary(t *testing.T) {
	lanes := []board.Lane{{Name: "backlog"}, {Name: "done", Done: true}}
	tasks := []*board.Task{
		{ID: "t-root", Title: "root", Status: "backlog", Priority: 10},
		{ID: "t-alien", Title: "lane removed from config", Status: "no-such-lane", Priority: 20, Deps: []string{"t-root"}},
	}
	b := board.NewStoreBoard(lanes, tasks, nil, true, "")

	m := New(memstore.New(), Options{})
	m.b = b
	m.w, m.h = 240, 60
	m.recompute()
	m.relayout()

	m.graphFocus, m.graphSel = "t-alien", "t-alien"
	m.view = viewGraph

	// Panicked here before the fix.
	out := m.View().Content
	if !strings.Contains(ansiStrip(out), "no-such-lane") {
		t.Error("the graph node did not render its off-vocabulary lane")
	}
}

// The epic id is an internal handle. Every other view resolves it to a title;
// the graph strip printed it raw.
func TestGraphStripResolvesTheEpicTitle(t *testing.T) {
	m := boardModel(t, 240, 60)
	var withEpic *board.Task
	for _, task := range m.b.Tasks() {
		if task.Epic != "" && m.b.Epic(task.Epic) != nil {
			withEpic = task
			break
		}
	}
	if withEpic == nil {
		t.Skip("no fixture task belongs to a resolvable epic")
	}
	m.selectID(withEpic.ID, false)
	m.openGraph()

	frame := ansiStrip(m.View().Content)
	if strings.Contains(frame, withEpic.Epic) {
		t.Errorf("the raw epic id %q leaked into the graph frame — every other view resolves it", withEpic.Epic)
	}

	// On the line that actually carries the strip's "epic " label, not merely
	// somewhere in the frame: the title also appears on cards, so a
	// frame-wide Contains passes against a strip that resolves nothing.
	title := m.b.Epic(withEpic.Epic).Title
	if title == "" {
		t.Skip("the fixture epic has no title to resolve")
	}
	// A needle long enough to be this epic's, not merely a common prefix:
	// "vista:" alone is shared by ten fixture titles.
	want := truncatedPrefix(title)
	for _, line := range strings.Split(frame, "\n") {
		if strings.Contains(line, "epic ") && strings.Contains(line, want) {
			return
		}
	}
	t.Errorf("no line carries both the strip's \"epic \" label and the resolved title %q", title)
}

// truncatedPrefix is the leading run of a title that survives any wrap or
// ellipsis the strip applies.
func truncatedPrefix(s string) string {
	r := []rune(s)
	if len(r) > 14 {
		r = r[:14]
	}
	return string(r)
}

var _ = tea.KeyPressMsg{}

// Through Update(), so the ROUTING is pinned too. Calling the handlers directly
// proves each one works but leaves onGraphKey free to wire ⏎ to graphBack and
// `<` to rerootGraph — a swap that survived the entire suite.
func TestGraphKeysAreRoutedToTheRightHandlers(t *testing.T) {
	m := graphModel(t, 240, 60)
	start := m.graphFocus

	moved := false
	for _, dir := range [][2]int{{0, -1}, {0, +1}, {+1, 0}, {-1, 0}} {
		m.graphMove(dir[0], dir[1])
		if m.graphSel != start {
			moved = true
			break
		}
	}
	if !moved {
		t.Skip("no reachable neighbour to move the graph selection to")
	}
	target := m.graphSel

	press(m, "enter") // must RE-ROOT, not retrace
	if m.graphFocus != target {
		t.Errorf("⏎ in the graph left the focus on %s, want %s — is it wired to graphBack?", m.graphFocus, target)
	}
	if len(m.graphStack) != 1 {
		t.Fatalf("⏎ did not push onto the walk stack: %v", m.graphStack)
	}

	press(m, "<") // must RETRACE, not re-root
	if m.graphFocus != start {
		t.Errorf("`<` retraced to %s, want %s — is it wired to rerootGraph?", m.graphFocus, start)
	}
	if len(m.graphStack) != 0 {
		t.Errorf("`<` left %d entries on the walk stack", len(m.graphStack))
	}

	// esc leaves the graph rather than doing anything inside it.
	press(m, "esc")
	if m.view != viewBoard {
		t.Errorf("esc left the view at %v", m.view)
	}
}

package ui

import (
	"fmt"
	"strings"
	"testing"

	lg "charm.land/lipgloss/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// mapModel opens the dep map and renders once, so the layout the key handlers
// walk is the one a frame was actually built from — the same setup contract
// graphModel has.
func mapModel(t *testing.T, w, h int) *Model {
	t.Helper()
	m := boardModel(t, w, h)
	m.openMap("")
	if _, err := m.Dump(w, h, "", true); err != nil {
		t.Fatal(err)
	}
	if m.mapLay == nil || len(m.mapLay.Rows) == 0 {
		t.Skip("the fixture board has no dependency clusters")
	}
	return m
}

// Every column is composed to exactly ColW display cells, which is the ONE
// rule that keeps a Japanese title from shearing the column to its right.
// Measured per panel line rather than per frame row: a frame row can be
// rectangular while the columns inside it have drifted, and the drift only
// becomes visible once a later panel is longer than an earlier one.
func TestMapPanelLinesAreExactlyOneColumnWide(t *testing.T) {
	for _, w := range []int{240, 241, 259, 320, 399, 400} {
		for _, scope := range []board.ClusterScope{board.ClusterOpen, board.ClusterAll} {
			m := boardModel(t, w, 50)
			m.mapScope = scope
			m.openMap("")
			l := m.buildMap()
			for _, p := range l.Panels {
				for j, line := range m.renderMapPanel(p, l.ColW) {
					if got := lg.Width(ansiStrip(line)); got != l.ColW {
						t.Errorf("w=%d scope=%s panel #%d line %d is %d cells, want ColW=%d: %q",
							w, scope, p.Num, j, got, l.ColW, ansiStrip(line))
					}
				}
			}
		}
	}
}

// The pack must place every cluster. Dropping one silently is the failure a
// grid layout invites, and the header would still count it.
func TestMapPacksEveryClusterExactlyOnce(t *testing.T) {
	m := boardModel(t, 400, 50)
	for _, scope := range []board.ClusterScope{board.ClusterOpen, board.ClusterAll} {
		m.mapScope = scope
		clusters := m.g.Clusters(scope)
		l := m.buildMap()
		if len(l.Panels) != len(clusters) {
			t.Fatalf("scope=%s packed %d of %d clusters", scope, len(l.Panels), len(clusters))
		}
		nodes := 0
		for _, c := range clusters {
			nodes += len(c.Nodes)
		}
		if len(l.Rows) != nodes {
			t.Errorf("scope=%s placed %d rows for %d cluster members", scope, len(l.Rows), nodes)
		}
		seen := map[string]bool{}
		for _, r := range l.Rows {
			if seen[r.ID] {
				t.Errorf("%s was placed twice", r.ID)
			}
			seen[r.ID] = true
			if r.Col < 0 || r.Col >= l.Cols {
				t.Errorf("%s landed in column %d of %d", r.ID, r.Col, l.Cols)
			}
		}
	}
}

// A board with one big tangle must spend the whole 240-400 cells on it. The
// first cut reserved a column per possible column and truncated every row to a
// third of the screen while two thirds stayed blank.
func TestOneClusterGetsTheWholeWidth(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.mapScope = board.ClusterAll
	l := m.buildMap()
	if len(l.Panels) != 1 {
		t.Skipf("this fixture has %d all-scope clusters, not one", len(l.Panels))
	}
	if l.Cols != 1 {
		t.Errorf("one cluster laid into %d columns", l.Cols)
	}
	if l.ColW < 200 {
		t.Errorf("the single panel is %d cells wide of an available %d", l.ColW, l.W)
	}
}

// The cursor walks rows within a column and crosses to the nearest row of the
// next one. Written against the handlers rather than through Update so a
// dropped arrow case cannot hide behind the dispatcher.
func TestMapCursorWalksRowsAndColumns(t *testing.T) {
	m := mapModel(t, 240, 50)
	l := m.mapLay

	first := l.Rows[0].ID
	m.mapSel = first
	m.mapMove(0, +1)
	if m.mapSel == first {
		t.Fatal("down did not move the cursor off the first row")
	}
	m.mapMove(0, -1)
	if m.mapSel != first {
		t.Errorf("down then up landed on %s, want %s", m.mapSel, first)
	}

	// Up from the top and down from the bottom stay put rather than wrapping:
	// a cursor that teleports across the screen is a re-ordering of the reader.
	m.mapSel = first
	m.mapMove(0, -1)
	if m.mapSel != first {
		t.Errorf("up from the top row moved to %s", m.mapSel)
	}

	if l.Cols < 2 {
		t.Skip("the fixture packs into one column; the sideways walk needs two")
	}
	m.mapSel = first
	m.mapMove(+1, 0)
	if got := l.Row(m.mapSel); got == nil || got.Col != 1 {
		t.Fatalf("right did not reach column 1, cursor is on %s", m.mapSel)
	}
	m.mapMove(-1, 0)
	if got := l.Row(m.mapSel); got == nil || got.Col != 0 {
		t.Errorf("left did not come back to column 0, cursor is on %s", m.mapSel)
	}
}

// A scope change removes whole clusters, so the cursor must land somewhere
// that still exists — and the strip below it must not be describing a task the
// grid no longer draws.
func TestScopeCycleKeepsTheCursorOnARowThatExists(t *testing.T) {
	m := mapModel(t, 240, 50)
	// t-t38k is done: present at scope=all, gone at scope=open.
	m.mapScope = board.ClusterAll
	m.mapSel = "t-t38k"
	if l := m.buildMap(); l.Row("t-t38k") == nil {
		t.Skip("this fixture does not carry t-t38k in an all-scope cluster")
	}
	m.cycleMapScope()
	if m.mapScope != board.ClusterOpen {
		t.Fatalf("scope cycled to %s, want open", m.mapScope)
	}
	l := m.buildMap()
	m.mapLay = l
	m.clampMapSel(l)
	if l.Row(m.mapSel) == nil {
		t.Errorf("the cursor stayed on %s, which scope=open does not draw", m.mapSel)
	}
	if m.mapScroll != 0 {
		t.Errorf("a scope change rebuilt the grid but kept scroll %d", m.mapScroll)
	}
}

// The blocker tag is the map's substitute for drawing a line, so it may never
// name a task that does not exist. Dropping ids is fine; cutting one in half
// is not.
func TestBlockerTagDropsWholeIDsAndSaysHowMany(t *testing.T) {
	m := boardModel(t, 240, 50)
	ids := []string{"t-aaaa", "t-bbbb", "t-cccc", "t-dddd"}

	full := ansiStrip(m.blockerTag(ids, 200))
	for _, id := range ids {
		if !strings.Contains(full, id) {
			t.Errorf("a roomy tag dropped %s: %q", id, full)
		}
	}
	if strings.Contains(full, "+") {
		t.Errorf("a roomy tag claims an overflow: %q", full)
	}

	tight := ansiStrip(m.blockerTag(ids, 14))
	if !strings.Contains(tight, "+") {
		t.Fatalf("a tight tag must say how many it dropped: %q", tight)
	}
	// Whatever survived must be a WHOLE id, never a prefix.
	names := strings.TrimPrefix(strings.SplitN(tight, " ", 2)[0], "←")
	for _, got := range strings.Split(names, ",") {
		if len(got) != len("t-aaaa") {
			t.Errorf("tag %q carries the fragment %q", tight, got)
		}
	}
	if strings.Contains(tight, "…") {
		t.Errorf("an id was elided rather than dropped: %q", tight)
	}
	if got := m.blockerTag(nil, 20); got != "" {
		t.Errorf("a node with no blockers gets no tag, got %q", got)
	}
}

// Opening the graph from the map is a detour INSIDE the overview: esc must
// come back to it, carrying wherever the graph walk ended.
func TestTheGraphOpenedFromTheMapReturnsToTheMap(t *testing.T) {
	m := mapModel(t, 240, 50)
	m.mapSel = "t-jv3j"
	if m.b.Task(m.mapSel) == nil {
		t.Skip("t-jv3j is not on the fixture board")
	}
	m.graphFromMap()
	if m.view != viewGraph {
		t.Fatalf("view is %s, want graph", m.view)
	}
	if m.graphFocus != "t-jv3j" {
		t.Errorf("the graph rooted on %s, want t-jv3j", m.graphFocus)
	}

	if _, err := m.Dump(240, 50, "", true); err != nil {
		t.Fatal(err)
	}
	m.graphMove(0, -1) // walk to a blocker
	ended := m.graphSel
	m.closeGraph()
	if m.view != viewMap {
		t.Fatalf("esc from a map-opened graph landed on %s, want the dep map", m.view)
	}
	if m.mapSel != ended {
		t.Errorf("the map cursor is on %s, want the node the walk ended on (%s)", m.mapSel, ended)
	}
	// And the next graph opened from the BOARD must still return to the board.
	m.closeMap()
	m.openGraph()
	m.closeGraph()
	if m.view != viewBoard {
		t.Errorf("a board-opened graph returned to %s, want the board", m.view)
	}
}

// Closing the map moves the board cursor to the row the walk ended on — the
// same contract as closing the graph, and the reason the map is navigation
// rather than a picture.
func TestClosingTheMapCarriesAWALKEDCursorToTheBoard(t *testing.T) {
	m := mapModel(t, 240, 50)
	m.mapMove(0, +1)
	walked := m.mapSel
	if !m.mapMoved {
		t.Fatal("setup: the cursor did not move")
	}
	m.closeMap()
	if m.view != viewBoard {
		t.Fatalf("view is %s, want board", m.view)
	}
	if got := m.curTask(); got == nil || got.ID != walked {
		t.Errorf("the board cursor is on %v, want %s", got, walked)
	}
	// An unfiltered walk must leave no permanent filter exemption behind.
	if len(m.pinned) != 0 {
		t.Errorf("closing the map pinned %v on an unfiltered board", m.pinned)
	}
}

// ...but a cursor the user never moved must NOT be carried back. Most of the
// board is in no cluster at all, so opening the map on such a task lands the
// cursor on a fallback row nobody chose; following that back to the board is a
// silent re-selection, which is exactly the failure frametruth_test.go names.
func TestAReadOnlyTripThroughTheMapDoesNotMoveTheBoardCursor(t *testing.T) {
	m := boardModel(t, 240, 50)

	var lone string
	for _, task := range m.b.Tasks() {
		if len(task.Deps) == 0 && len(m.g.Blocks(task.ID)) == 0 {
			lone = task.ID
			break
		}
	}
	if lone == "" {
		t.Skip("every fixture task has a dep edge")
	}
	if !m.selectID(lone, false) {
		t.Fatalf("setup: could not select %s", lone)
	}

	m.openMap(lone)
	if _, err := m.Dump(240, 50, "", true); err != nil {
		t.Fatal(err)
	}
	if m.mapSel == lone {
		t.Skipf("%s turned out to be in a cluster", lone)
	}
	if !strings.Contains(ansiStrip(m.statusLine()), lone) {
		t.Errorf("the map moved the cursor off %s without saying so: %q",
			lone, ansiStrip(m.statusLine()))
	}
	m.closeMap()
	if got := m.curTask(); got == nil || got.ID != lone {
		t.Errorf("a read-only T/esc round trip moved the board cursor from %s to %v", lone, got)
	}
}

// The note openMap prints must say WHY the seed is missing. A task whose deps
// are all done is not a task without dependencies, and the flat sentence was a
// falsehood for every done task with deps.
func TestOpenMapNamesWhyTheSeedIsNotOnTheMap(t *testing.T) {
	m := boardModel(t, 240, 50)
	// t-t38k is done and has three deps: present at scope=all, absent at open.
	if task := m.b.Task("t-t38k"); task == nil || len(task.Deps) == 0 {
		t.Skip("the fixture has no done task carrying deps")
	}
	m.openMap("t-t38k")
	got := ansiStrip(m.statusLine())
	if strings.Contains(got, "has no dependencies") {
		t.Errorf("t-t38k has 3 deps and the map says it has none: %q", got)
	}
	if !strings.Contains(got, "z") {
		t.Errorf("the note does not point at the key that would show it: %q", got)
	}
}

// The map shows what the board filter HIDES — an edge that vanishes because of
// a query is a lie about the board — so filtered rows are marked and counted,
// never dropped.
func TestTheFilterMarksMapRowsRatherThanRemovingThem(t *testing.T) {
	m := New(memstore.New(), Options{})
	if _, err := m.Dump(240, 50, "", true); err != nil {
		t.Fatal(err)
	}
	before := len(m.buildMap().Rows)

	if _, err := m.Dump(240, 50, "mapfiltered", true); err != nil {
		t.Fatal(err)
	}
	l := m.buildMap()
	if len(l.Rows) != before {
		t.Errorf("a filter removed %d map rows; the map draws structure, not the query",
			before-len(l.Rows))
	}
	hidden := m.mapHiddenCount(l)
	if hidden == 0 {
		t.Fatal("setup: this filter hides no cluster member, so the marking is untested")
	}
	if !strings.Contains(ansiStrip(m.mapHeader(l, false)), "hidden by the filter") {
		t.Errorf("%d rows are hidden by the filter and the header does not say so", hidden)
	}
}

// An edgeless board is healthy, not broken: the map must say so in words
// rather than drawing an empty grid.
func TestAnEmptyMapExplainsItself(t *testing.T) {
	b := board.NewBoard([]*board.Task{
		{ID: "t-solo", Title: "no deps at all", Status: "backlog"},
	})
	m := New(memstore.NewWith(b), Options{})
	m.openMap("")
	out, err := m.Dump(240, 50, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "no open dependency clusters") {
		t.Errorf("an edgeless board draws no explanation:\n%s", out)
	}
	if m.mapSel != "" {
		t.Errorf("the cursor is on %q with nothing to select", m.mapSel)
	}
}

// The selected row must be marked in the frame. A selection nothing draws is
// exactly the model/frame disagreement frametruth_test.go exists for.
func TestTheSelectedMapRowIsMarkedInTheFrame(t *testing.T) {
	m := mapModel(t, 240, 50)
	l := m.mapLay
	m.mapSel = l.Rows[len(l.Rows)-1].ID

	out, err := m.Dump(240, 50, "", true)
	if err != nil {
		t.Fatal(err)
	}
	marked := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "▌") {
			marked++
			if !strings.Contains(line, m.mapSel) {
				t.Errorf("the selection gutter is on a row that is not %s: %q",
					m.mapSel, strings.TrimSpace(line))
			}
		}
	}
	if marked != 1 {
		t.Errorf("%d rows carry the selection gutter, want exactly 1", marked)
	}
	// And the strip below describes that same task.
	if !strings.Contains(out, m.b.Task(m.mapSel).Title) {
		t.Errorf("the strip does not carry the selected task's title")
	}
}

// A row deeper in the chain is drawn further right, and the ladder is capped
// so a pathological chain eats its own title rather than the whole panel.
func TestIndentFollowsDepthAndIsCapped(t *testing.T) {
	if mapIndent(0) != 0 {
		t.Errorf("a root is not indented, got %d", mapIndent(0))
	}
	if mapIndent(3) != 3*mapIndentW {
		t.Errorf("depth 3 indent = %d, want %d", mapIndent(3), 3*mapIndentW)
	}
	if got := mapIndent(mapMaxIndent + 50); got != mapMaxIndent*mapIndentW {
		t.Errorf("a runaway chain indents %d cells, want the %d cap",
			got, mapMaxIndent*mapIndentW)
	}
}

// The scroll offset is computed from the SAME Y the renderer placed the row
// at, so "the cursor is off screen" cannot happen while the frame says it is
// visible.
func TestMapScrollFollowsTheCursor(t *testing.T) {
	m := boardModel(t, 240, 24) // short enough that the all-scope tangle clips
	m.mapScope = board.ClusterAll
	m.openMap("")
	l := m.buildMap()
	if l.H <= m.mapCanvasH() {
		t.Skip("the fixture's clusters fit without scrolling at this height")
	}
	last := l.Rows[len(l.Rows)-1]
	m.mapSel = last.ID
	got := m.scrollMapToSel(l, l.H, m.mapCanvasH())
	if last.Y < got || last.Y >= got+m.mapCanvasH() {
		t.Errorf("row %s sits at Y=%d, outside the window [%d,%d)",
			last.ID, last.Y, got, got+m.mapCanvasH())
	}
	first := l.Rows[0]
	m.mapSel = first.ID
	m.mapScroll = got
	if s := m.scrollMapToSel(l, l.H, m.mapCanvasH()); first.Y < s {
		t.Errorf("scrolling back to row %s left it above the window at %d", first.ID, s)
	}
}

// Every one of the map's bindings driven through Update, because the help
// overlay only proves a key is LISTED. Review found all five — T from the
// board, T from the graph, z, ⏎, and the onKey route itself — could be cut
// while the whole suite stayed green.
func TestTheMapKeysAreActuallyBound(t *testing.T) {
	m := boardModel(t, 240, 50)
	if _, err := m.Dump(240, 50, "", true); err != nil {
		t.Fatal(err)
	}

	m.Update(keyMsg("T"))
	if m.view != viewMap {
		t.Fatalf("T on the board did not open the dep map, view is %s", m.view)
	}
	if _, err := m.Dump(240, 50, "", true); err != nil {
		t.Fatal(err)
	}
	if len(m.mapLay.Rows) == 0 {
		t.Skip("the fixture board has no dependency clusters")
	}

	m.Update(keyMsg("z"))
	if m.mapScope != board.ClusterAll {
		t.Errorf("z did not cycle the scope, it is %s", m.mapScope)
	}
	m.Update(keyMsg("z"))
	if m.mapScope != board.ClusterOpen {
		t.Errorf("z did not cycle back, the scope is %s", m.mapScope)
	}

	// The arrows are the map's, not the board's: the route in onKey must send
	// them to onMapKey. Deleting that route left every test green because `?`
	// is handled the same way in both.
	if _, err := m.Dump(240, 50, "", true); err != nil {
		t.Fatal(err)
	}
	before := m.mapSel
	beforeLane, beforeIdx := m.curLane, m.curIdx[m.curLaneName()]
	m.Update(keyMsg("j"))
	if m.mapSel == before {
		t.Error("j did not move the map cursor — is the viewMap route in onKey there?")
	}
	if m.curLane != beforeLane || m.curIdx[m.curLaneName()] != beforeIdx {
		t.Error("j moved the BOARD cursor while the map was up")
	}

	m.Update(keyMsg("enter"))
	if m.view != viewGraph {
		t.Fatalf("⏎ on a map row did not open the graph, view is %s", m.view)
	}
	if _, err := m.Dump(240, 50, "", true); err != nil {
		t.Fatal(err)
	}
	m.Update(keyMsg("T"))
	if m.view != viewMap {
		t.Errorf("T in the graph did not open the dep map, view is %s", m.view)
	}
	m.Update(keyMsg("esc"))
	if m.view != viewBoard {
		t.Errorf("esc in the dep map did not return to the board, view is %s", m.view)
	}
}

// fullScreen() is what tells the board's modals they are not on screen. It had
// no coverage at all — for the graph either — so dropping its viewMap arm was
// invisible, and a refused quick-add would reopen its modal into a view that
// composites nothing (the failure t-74y3 recorded for the graph).
func TestAFullScreenViewRefusesToReopenAnInvisibleModal(t *testing.T) {
	m := boardModel(t, 240, 50)
	for _, tc := range []struct {
		name string
		open func()
	}{
		{"graph", func() { m.openGraph() }},
		{"map", func() { m.openMap("") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m.view = viewBoard
			m.mode = modeNormal
			tc.open()
			if !m.fullScreen() {
				t.Fatalf("%s does not report itself full-screen", tc.name)
			}
			if c := m.reopenRefusedAdd(persistOp{addTitle: "held"}); c != nil || m.mode == modeAdd {
				t.Errorf("a refused add reopened its modal over the %s, which draws no modal layer", tc.name)
			}
		})
	}
}

// The panel's stat line and header are the whole reason to look at an overview,
// and every number in them survived deletion. Pinned against the fixture, and
// cross-checked against the row markers in the SAME frame: at scope=all the
// counts used to be pure topology and contradicted the `v`/`x` glyphs beside
// them.
func TestTheHeadlineNumbersMatchTheRowsTheySitUnder(t *testing.T) {
	for _, scope := range []board.ClusterScope{board.ClusterOpen, board.ClusterAll} {
		m := boardModel(t, 240, 60)
		m.mapScope = scope
		m.openMap("")
		l := m.buildMap()
		if len(l.Panels) == 0 {
			t.Skip("no clusters on the fixture board")
		}
		for _, p := range l.Panels {
			c := p.Cluster
			if got := c.Roots() + c.Blocked() + c.Done(); got != len(c.Nodes) {
				t.Errorf("scope=%s #%d: %d unblocked + %d blocked + %d done = %d, want %d members",
					scope, p.Num, c.Roots(), c.Blocked(), c.Done(), got, len(c.Nodes))
			}
			// Every count must be readable off the markers the rows draw.
			blocked, done := 0, 0
			for _, n := range c.Nodes {
				glyph, _ := cardMarker(m.b.Task(n.ID), m.g)
				switch glyph {
				case glyphBlocked:
					blocked++
				case glyphDone:
					done++
				}
			}
			if blocked != c.Blocked() {
				t.Errorf("scope=%s #%d says %d blocked, the rows draw %d x markers",
					scope, p.Num, c.Blocked(), blocked)
			}
			if done != c.Done() {
				t.Errorf("scope=%s #%d says %d done, the rows draw %d v markers",
					scope, p.Num, c.Done(), done)
			}
			if top := c.Top(); top.ID != "" && m.g.IsDone(top.ID) {
				t.Errorf("scope=%s #%d names the finished %s as the task to close",
					scope, p.Num, top.ID)
			}
			// And it must actually be RENDERED, not merely computed.
			line := ansiStrip(m.renderMapPanel(p, l.ColW)[p.H-1])
			if !strings.Contains(line, fmt.Sprintf("%d unblocked", c.Roots())) ||
				!strings.Contains(line, fmt.Sprintf("%d blocked", c.Blocked())) {
				t.Errorf("scope=%s #%d stat line %q carries neither count", scope, p.Num, line)
			}
			hdr := ansiStrip(m.renderMapPanel(p, l.ColW)[0])
			if !strings.Contains(hdr, fmt.Sprintf("#%d", p.Num)) ||
				!strings.Contains(hdr, fmt.Sprintf("depth %d", c.Depth())) {
				t.Errorf("scope=%s panel rule %q does not name the cluster", scope, hdr)
			}
		}
	}
}

// The seed openMap is handed must actually be honoured — discarding it left
// every test green because they all opened with "".
func TestOpenMapLandsOnItsSeed(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.openMap("t-rmtc")
	if m.buildMap().Row("t-rmtc") == nil {
		t.Skip("t-rmtc is in no open cluster on this fixture")
	}
	if m.mapSel != "t-rmtc" {
		t.Errorf("openMap seeded with t-rmtc landed on %q", m.mapSel)
	}
	if m.mapMoved {
		t.Error("opening the map counts as a cursor move the board should follow")
	}
}

// ^u/^d is advertised in the map's own header and in the `?` overlay. It was
// inert: renderMap re-pins the window to the cursor every frame, so nudging
// the offset snapped straight back.
func TestPagingTheMapActuallyMovesTheView(t *testing.T) {
	m := boardModel(t, 240, 22)
	m.mapScope = board.ClusterAll
	m.openMap("")
	if _, err := m.Dump(240, 22, "", true); err != nil {
		t.Fatal(err)
	}
	if m.mapLay.H <= m.mapCanvasH() {
		t.Skip("the fixture's clusters fit without paging at this height")
	}
	// The first press need not scroll — half a page of cursor can still land
	// inside the window. What must not happen is the window never moving at
	// all, which is what re-pinning the offset every frame produced.
	first, firstScroll := m.mapSel, m.mapScroll
	for i := 0; i < 3; i++ {
		m.Update(keyMsg("ctrl+d"))
		if _, err := m.Dump(240, 22, "", true); err != nil {
			t.Fatal(err)
		}
	}
	if m.mapScroll == firstScroll {
		t.Errorf("three ^d left the window at row %d of %d — the advertised key does nothing",
			m.mapScroll, m.mapLay.H)
	}
	if m.mapSel == first {
		t.Error("^d did not move the cursor")
	}
	for i := 0; i < 3; i++ {
		m.Update(keyMsg("ctrl+u"))
		if _, err := m.Dump(240, 22, "", true); err != nil {
			t.Fatal(err)
		}
	}
	if m.mapScroll != firstScroll {
		t.Errorf("^u did not come back to row %d, it is at %d", firstScroll, m.mapScroll)
	}
	if m.mapSel != first {
		t.Errorf("^u did not come back to %s, the cursor is on %s", first, m.mapSel)
	}
}

// A strip too short for a single content row must render nothing, not panic.
// `-dump -demo map -rows 7` is the height where stripHeight lands on 1.
func TestAShortTerminalDoesNotPanicInEitherFullScreenView(t *testing.T) {
	for _, demo := range []string{"map", "mapall", "mapfiltered", "graph", "graphall"} {
		for h := 4; h <= 12; h++ {
			// Both graph orientations: they negotiate opposite axes, so the
			// height that collapses one is not the height that collapses the
			// other.
			for _, lr := range []bool{false, true} {
				m := New(memstore.New(), Options{GraphLR: lr})
				if _, err := m.Dump(240, h, demo, true); err != nil {
					t.Errorf("-demo %s -rows %d graphlr=%v: %v", demo, h, lr, err)
				}
			}
		}
	}
}

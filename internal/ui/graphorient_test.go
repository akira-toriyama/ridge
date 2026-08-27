package ui

import (
	"fmt"
	"strings"
	"testing"

	lg "charm.land/lipgloss/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// The graph can be drawn top-down or left-right. These pin the two halves of
// that: the axis mapping is a pure rotation, and neither half lies about the
// board.

// graph.go's doc comment has always cited this test as the reason only
// single-width runes may reach the rune grid. It did not exist. It matters now
// for a second reason: left-right adds a glyph to the set, and a Wide one would
// shear every band to its right instead of failing anywhere visible.
func TestEdgeCanvasGlyphsAreSingleWidth(t *testing.T) {
	for mask, r := range junction {
		if r == 0 {
			continue
		}
		if w := lg.Width(string(r)); w != 1 {
			t.Errorf("junction[%d] = %q measures %d cells, want 1", mask, r, w)
		}
	}
	for _, o := range []graphOrient{orientTopDown, orientLeftRight} {
		a := graphArrow(o)
		if w := lg.Width(string(a)); w != 1 {
			t.Errorf("%s arrowhead %q measures %d cells, want 1", o, a, w)
		}
	}
	if graphArrow(orientTopDown) == graphArrow(orientLeftRight) {
		t.Error("both orientations point with the same glyph — the arrowhead is half the direction contract")
	}
}

// rotMask maps a top-down direction bit to the left-right bit that means the
// same thing about the LAYOUT: across-decreasing is "up" drawn top-down and
// "left" drawn left-right, along-increasing is "right" then "down".
func rotMask(m uint8) uint8 {
	var out uint8
	for from, to := range map[uint8]uint8{dirN: dirW, dirS: dirE, dirE: dirS, dirW: dirN} {
		if m&from != 0 {
			out |= to
		}
	}
	return out
}

// The two orientations must be the SAME channel, transposed — not two routers
// that happen to look alike. Anything that drifts (a bus offset counted from
// the wrong end, an arrowhead one cell early) shows up here as a cell that does
// not rotate onto its partner.
func TestChannelIsTheSameShapeOnBothAxes(t *testing.T) {
	l := spanningLayout(t)
	along := 60
	l.place(orientTopDown, along)

	for r := 0; r+1 < len(l.Layers); r++ {
		edges := l.rankEdges(r)
		routes, depth := routeChannel(l, edges)
		td := drawChannel(orientTopDown, along, routes, depth)
		lr := drawChannel(orientLeftRight, along, routes, depth)

		if td.w != along || td.h != depth {
			t.Fatalf("channel %d top-down canvas is %dx%d, want %dx%d", r, td.w, td.h, along, depth)
		}
		if lr.w != depth || lr.h != along {
			t.Fatalf("channel %d left-right canvas is %dx%d, want %dx%d", r, lr.w, lr.h, depth, along)
		}
		for a := 0; a < along; a++ {
			for c := 0; c < depth; c++ {
				want := rotMask(td.mask[c*td.w+a])
				if got := lr.mask[a*lr.w+c]; got != want {
					t.Errorf("channel %d cell along=%d across=%d: mask %04b, want %04b (rotated)",
						r, a, c, got, want)
				}
				tdG, lrG := td.glyph[c*td.w+a], lr.glyph[a*lr.w+c]
				if (tdG == 0) != (lrG == 0) {
					t.Errorf("channel %d cell along=%d across=%d: glyph %q vs %q", r, a, c, tdG, lrG)
				}
				if tdG == glyphArrowDown && lrG != glyphArrowRight {
					t.Errorf("channel %d cell along=%d across=%d: left-right arrowhead is %q", r, a, c, lrG)
				}
			}
		}
	}
}

// spanningLayout is an ego graph with an edge that SPANS a rank, so the routing
// dummy and the pass-through drawing are actually exercised. No fixture task
// produces one — measured over every fixture id at every radius — so without
// this the whole dummy path ships unverified in either orientation.
func spanningLayout(t *testing.T) *egoLayout {
	t.Helper()
	g := board.NewGraph(spanningBoard())
	l := buildEgo(g, "t-c", graphAllRadius, graphHardCols, nil)
	dummies := 0
	for _, n := range l.Nodes {
		if n.Kind == egoDummy {
			dummies++
		}
	}
	if dummies == 0 {
		t.Fatal("the spanning fixture produced no routing dummy, so it proves nothing")
	}
	return l
}

// t-a → t-b → t-c, and t-a → t-c as well: longest-path layering puts t-a two
// ranks above t-c, so the direct edge has to be carried through the rank in
// between.
func spanningBoard() *board.Board {
	return board.NewBoard([]*board.Task{
		{ID: "t-a", Title: "根の作業 — 上流に何も持たない", Status: "backlog", Priority: 10},
		{ID: "t-b", Title: "中継の作業 — 根だけを待つ", Status: "backlog", Priority: 20, Deps: []string{"t-a"}},
		{ID: "t-c", Title: "末端の作業 — 根と中継の両方を待つ", Status: "backlog", Priority: 30,
			Deps: []string{"t-a", "t-b"}},
	})
}

// The pass-through has to READ as a pass-through in both orientations: a line
// continuing through the rank, not an arrowhead landing on nothing.
func TestSpanningEdgeDrawsAPassThroughInBothOrientations(t *testing.T) {
	for _, tc := range []struct {
		orient graphOrient
		rule   string
	}{
		{orientTopDown, "│"},
		{orientLeftRight, "─"},
	} {
		m := New(memstore.NewWith(spanningBoard()), Options{GraphLR: tc.orient == orientLeftRight})
		m.selectID("t-c", false)
		m.openGraph()
		m.graphRadius = graphAllRadius
		out, err := m.Dump(240, 50, "", true)
		if err != nil {
			t.Fatalf("%s: %v", tc.orient, err)
		}
		if !strings.Contains(out, tc.rule) {
			t.Errorf("%s: no pass-through rule %q in the frame", tc.orient, tc.rule)
		}
		for _, id := range []string{"t-a", "t-b", "t-c"} {
			if !strings.Contains(out, id) {
				t.Errorf("%s: %s is missing from the frame", tc.orient, id)
			}
		}
		arrows := strings.Count(out, string(graphArrow(tc.orient)))
		if arrows != 2 {
			// t-a→t-b and t-b→t-c land; t-a→t-c passes THROUGH rank 1 and
			// lands once at the end, so an extra head means the dummy grew one.
			t.Errorf("%s: %d arrowheads, want 2 (the third edge arrives through the dummy)", tc.orient, arrows)
		}
	}
}

// Rotating the picture must not change what the picture is OF. The header
// counts nodes and edges, and a cap read off the screen rather than off the
// graph would make `o` quietly drop dependencies.
func TestOrientationDoesNotChangeWhatTheGraphContains(t *testing.T) {
	for _, wh := range [][2]int{{240, 50}, {240, 30}, {320, 70}, {400, 90}} {
		m := graphModel(t, wh[0], wh[1])
		m.graphRadius = graphAllRadius
		m.View()
		td := m.graphLay

		m.cycleGraphOrient()
		m.View()
		lr := m.graphLay

		if a, b := len(td.Real()), len(lr.Real()); a != b {
			t.Errorf("%dx%d: %d nodes top-down, %d left-right", wh[0], wh[1], a, b)
		}
		if a, b := len(td.Edges), len(lr.Edges); a != b {
			t.Errorf("%dx%d: %d edges top-down, %d left-right", wh[0], wh[1], a, b)
		}
		if a, b := len(td.Layers), len(lr.Layers); a != b {
			t.Errorf("%dx%d: %d ranks top-down, %d left-right", wh[0], wh[1], a, b)
		}
	}
}

// The left-right frame spends its width on LAYERS, so the node box is the thing
// that gets squeezed — and joinEnds drops the left end of a line first. Below
// the floor width a box stops printing the id `⏎` re-roots on, which is the one
// thing it must always say.
func TestEveryLeftRightNodeBoxPrintsItsID(t *testing.T) {
	for _, wh := range [][2]int{{240, 50}, {240, 40}, {320, 70}, {400, 90}} {
		m := New(memstore.New(), Options{GraphLR: true})
		out, err := m.Dump(wh[0], wh[1], "graphall", true)
		if err != nil {
			t.Fatalf("%dx%d: %v", wh[0], wh[1], err)
		}
		canvas := canvasOf(m, out)
		for _, n := range m.graphLay.Real() {
			if !strings.Contains(canvas, n.ID) {
				t.Errorf("%dx%d: node %s is drawn but its id is not in the drawing", wh[0], wh[1], n.ID)
			}
		}
		fitsTheWidth(t, m, m.graphMeasure(m.graphLay))
		if m.graphLay.Span-graphNodeChrome < graphMinNodeLines {
			t.Errorf("%dx%d: the node span left no title line at all", wh[0], wh[1])
		}
	}
}

// Columns composed side by side accumulate a one-cell shear per column, so it
// hides at one width and shows at another. The map's demos are in the frame
// test for exactly this reason; the left-right graph is the second view built
// that way.
func TestLeftRightFrameStaysRectangular(t *testing.T) {
	for _, w := range []int{240, 241, 259, 320, 399, 400} {
		for _, h := range []int{24, 40, 50, 90} {
			for _, demo := range []string{"graph", "graphall"} {
				m := New(memstore.New(), Options{GraphLR: true})
				out, err := m.Dump(w, h, demo, true)
				if err != nil {
					t.Fatalf("%dx%d %s: %v", w, h, demo, err)
				}
				for i, line := range strings.Split(out, "\n") {
					if got := lg.Width(line); got != w {
						t.Errorf("%dx%d %s: row %d is %d cells wide", w, h, demo, i, got)
					}
				}
			}
		}
	}
}

// A short terminal must scroll the left-right frame, not silently drop the
// nodes the other orientation would have shown. The along axis is the screen
// line axis here, so the existing scroll is the whole mechanism — this pins
// that it is actually reached.
func TestAShortTerminalScrollsTheLeftRightFrameRatherThanDroppingNodes(t *testing.T) {
	tall := New(memstore.New(), Options{GraphLR: true})
	if _, err := tall.Dump(240, 90, "graphall", true); err != nil {
		t.Fatal(err)
	}
	want := len(tall.graphLay.Real())

	short := New(memstore.New(), Options{GraphLR: true})
	if _, err := short.Dump(240, 24, "graphall", true); err != nil {
		t.Fatal(err)
	}
	if got := len(short.graphLay.Real()); got != want {
		t.Errorf("a 24-row terminal drew %d nodes, a 90-row one %d — the short frame dropped work", got, want)
	}
	// Not a second Dump: Dump re-applies the demo, and the demo re-roots the
	// graph, which resets the scroll it is trying to observe.
	before := frame(short)
	short.Update(ctrlD())
	if after := frame(short); before == after {
		t.Error("^d changed nothing in a left-right frame taller than its canvas")
	}
}

// `o` is the whole feature's entry point, and the scroll offset counts lines of
// a frame laid out on the other axis — carrying it over lands the window
// somewhere nothing chose.
func TestOrientationKeyFlipsTheAxisAndDropsTheScroll(t *testing.T) {
	m := graphModel(t, 240, 40)
	if m.graphOrient != orientTopDown {
		t.Fatalf("the graph opened %s; top-down is the default the docs describe", m.graphOrient)
	}
	m.graphScroll = 7
	press(m, "o")
	if m.graphOrient != orientLeftRight {
		t.Errorf("`o` left the graph %s", m.graphOrient)
	}
	if m.graphScroll != 0 {
		t.Errorf("`o` carried a scroll offset of %d across the flip", m.graphScroll)
	}
	press(m, "o")
	if m.graphOrient != orientTopDown {
		t.Errorf("`o` did not flip back: %s", m.graphOrient)
	}
}

// The header's two direction words are half the position-plus-arrowhead
// redundancy. A header still saying "↑ blockers" over a left-right picture
// would be the reader's only cue, pointing the wrong way.
func TestTheHeaderNamesTheAxisItIsDrawnOn(t *testing.T) {
	td := strings.Join(dumpFrame(t, 240, 50, "graph"), "\n")
	if !strings.Contains(td, "↑ blockers") || !strings.Contains(td, "↓ unblocks") {
		t.Error("the top-down header lost its direction words")
	}
	m := New(memstore.New(), Options{GraphLR: true})
	out, err := m.Dump(240, 50, "graph", true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "← blockers") || !strings.Contains(out, "→ unblocks") {
		t.Error("the left-right header still points up and down")
	}
	if strings.Contains(out, "↑ blockers") {
		t.Error("the left-right header carries the top-down words too")
	}
}

// The arrows cross layers on whichever axis the layers run along, so ↓ always
// means "further from the blockers" whichever way the picture is drawn.
func TestGraphMoveCrossesLayersOnTheAxisTheyRunAlong(t *testing.T) {
	for _, tc := range []struct {
		orient  graphOrient
		dx, dy  int
		crosses bool
	}{
		{orientTopDown, 0, 1, true},
		{orientTopDown, 1, 0, false},
		{orientLeftRight, 1, 0, true},
		{orientLeftRight, 0, 1, false},
	} {
		m := graphModel(t, 240, 50)
		m.graphOrient = tc.orient
		m.View()
		start := m.graphLay.Node(m.graphSel)
		if start == nil {
			t.Fatal("no selection to walk from")
		}
		m.graphMove(tc.dx, tc.dy)
		got := m.graphLay.Node(m.graphSel)
		if got == nil {
			t.Fatalf("%s: the walk left the selection off the layout", tc.orient)
		}
		if moved := got.Rank != start.Rank; moved != tc.crosses {
			t.Errorf("%s: move(%d,%d) rank %d→%d, crossesLayers=%v want %v",
				tc.orient, tc.dx, tc.dy, start.Rank, got.Rank, moved, tc.crosses)
		}
	}
}

// A focus with no structure says so in words in both orientations — and the
// words go where the missing structure would have been.
func TestAnIsolatedFocusSaysSoInBothOrientations(t *testing.T) {
	b := board.NewBoard([]*board.Task{
		{ID: "t-lonely", Title: "誰も待たず誰にも待たれない作業", Status: "backlog", Priority: 10},
	})
	for _, lr := range []bool{false, true} {
		m := New(memstore.NewWith(b), Options{GraphLR: lr})
		m.selectID("t-lonely", false)
		m.openGraph()
		out, err := m.Dump(240, 50, "", true)
		if err != nil {
			t.Fatalf("lr=%v: %v", lr, err)
		}
		for _, want := range []string{"no blockers", "waits on nothing", "t-lonely"} {
			if !strings.Contains(out, want) {
				t.Errorf("lr=%v: the isolated frame never says %q", lr, want)
			}
		}
	}
}

// chainBoard is a straight chain of n tasks, so the ego graph has one node per
// rank and as many ranks as the chain is long. The fixture's deepest graph is
// 6 ranks and fits the 240-column floor exactly, which means nothing in it ever
// reaches the left-right frame's two width defences — the node-width floor and
// the rank window. This does.
func chainBoard(n int) *board.Board {
	tasks := make([]*board.Task, 0, n)
	for i := 0; i < n; i++ {
		t := &board.Task{
			ID:       fmt.Sprintf("t-c%02d", i),
			Title:    fmt.Sprintf("鎖の%d番目 — 前の一つだけを待って次の一つを塞ぐ作業", i),
			Status:   "backlog",
			Priority: (i + 1) * 10,
			Repos:    []string{"tomo/kyushu-trip"},
		}
		if i > 0 {
			t.Deps = []string{fmt.Sprintf("t-c%02d", i-1)}
		}
		tasks = append(tasks, t)
	}
	return board.NewBoard(tasks)
}

// canvasOf is the DRAWING only — no title bar, no header, no strip, no status.
// Searching a whole frame for an id is no test at all: the header names the
// root and the strip names the selection, so a box that truncated its own id
// still "contains" it.
func canvasOf(m *Model, out string) string {
	lines := strings.Split(out, "\n")
	lo := minInt(fullTop, len(lines))
	hi := minInt(fullTop+m.graphCanvasH(), len(lines))
	return strings.Join(lines[lo:hi], "\n")
}

// fitsTheWidth is the invariant every left-right frame owes: the bands it means
// to draw actually fit the drawing, so nothing is silently truncated off the
// right edge by the pad in renderGraph.
func fitsTheWidth(t *testing.T, m *Model, f graphFrame) {
	t.Helper()
	total := (f.last - f.first + 1) * f.nodeW
	for r := f.first; r < f.last; r++ {
		total += f.channels[r]
	}
	if total > m.graphWidth() {
		t.Errorf("the bands add up to %d cells in a %d-cell drawing — the right edge is cut",
			total, m.graphWidth())
	}
}

func chainGraph(t *testing.T, n, w, h int) (*Model, string) {
	t.Helper()
	m := New(memstore.NewWith(chainBoard(n)), Options{GraphLR: true})
	m.selectID(fmt.Sprintf("t-c%02d", n/2), false)
	m.openGraph()
	m.graphRadius = graphAllRadius
	out, err := m.Dump(w, h, "", true)
	if err != nil {
		t.Fatalf("%d-deep chain at %dx%d: %v", n, w, h, err)
	}
	return m, out
}

// A chain deeper than the width can hold is the only thing that reaches the
// left-right frame's two width defences, and it must trip BOTH: boxes stay at
// or above the floor that keeps their id readable, and the ranks that no longer
// fit are dropped and REPORTED rather than truncated off the right edge.
func TestADeepChainKeepsItsBoxesReadableAndSaysWhatItDropped(t *testing.T) {
	m, out := chainGraph(t, 12, 240, 50)
	l, f := m.graphLay, m.graphMeasure(m.graphLay)

	// The consequence the floor exists for, not the floor itself: a box that
	// cannot hold the median Japanese title (82 cells, CLAUDE.md) is not worth
	// the layer it costs. Asserting f.nodeW >= graphNodeMinWLR would just
	// restate the constant.
	if budget := f.titleLines * (f.nodeW - 4); budget < 82 {
		t.Errorf("a node box holds %d cells of title (%d lines x %d inner); the median "+
			"Japanese title is 82", budget, f.titleLines, f.nodeW-4)
	}
	if !strings.Contains(canvasOf(m, out), "kyushu-trip") {
		t.Error("no box kept its repo chip — the id line is down to bare identity")
	}
	fitsTheWidth(t, m, f)
	if f.hidden == 0 {
		t.Fatalf("a 12-rank chain fitted %d ranks in 240 columns with nothing dropped — "+
			"this test no longer reaches the rank window", len(l.Layers))
	}
	if got, want := f.hidden+(f.last-f.first+1), len(l.Layers); got != want {
		t.Errorf("%d drawn + %d hidden = %d, but the graph has %d ranks", f.last-f.first+1, f.hidden, got, want)
	}
	if !strings.Contains(out, "beyond the width") {
		t.Error("ranks were dropped and the header never said so")
	}
	canvas := canvasOf(m, out)
	for r := f.first; r <= f.last; r++ {
		for _, n := range l.Layers[r] {
			if n.Kind == egoReal && !strings.Contains(canvas, n.ID) {
				t.Errorf("rank %d is inside the drawn window but %s is not in the drawing", r, n.ID)
			}
		}
	}
	// The focus must be inside its own window, whichever way the growth went.
	if fr := l.FocusRank(); fr < f.first || fr > f.last {
		t.Errorf("the focus sits on rank %d, outside the drawn window %d..%d", fr, f.first, f.last)
	}
}

// A pass-through is ONE rule on the row the channel attaches to. Give it a
// box's worth of rows and the rule is drawn on one row while the line arrives
// on another — a visible break in an edge the graph promised to draw.
func TestAPassThroughSitsOnTheRowItsChannelAttachesTo(t *testing.T) {
	l := spanningLayout(t)
	for _, o := range []graphOrient{orientTopDown, orientLeftRight} {
		l.place(o, 60)
		_, _, _, dummy := nodeSpans(o)
		for _, n := range l.Nodes {
			if n.Kind != egoDummy {
				continue
			}
			if n.Span != dummy {
				t.Errorf("%s: a dummy spans %d, want %d", o, n.Span, dummy)
			}
			if o == orientLeftRight && n.Anchor() != n.Along {
				t.Errorf("left-right: the pass-through rule is drawn on row %d but the channel "+
					"attaches at %d", n.Along, n.Anchor())
			}
		}
	}
}

// handChannel is two sources fanning into one target, laid out BY HAND so the
// expected picture below is a literal rather than something recomputed from the
// code under test.
func handChannel() (*egoLayout, []routedEdge, int) {
	l := &egoLayout{Nodes: map[string]*egoNode{
		"a": {Key: "a", Kind: egoReal, Rank: 0, Along: 0, Span: 4},
		"b": {Key: "b", Kind: egoReal, Rank: 0, Along: 8, Span: 4},
		"c": {Key: "c", Kind: egoReal, Rank: 1, Along: 4, Span: 4},
	}}
	l.Edges = []egoEdge{{From: "a", To: "c"}, {From: "b", To: "c"}}
	routes, depth := routeChannel(l, l.Edges)
	return l, routes, depth
}

// The one ABSOLUTE assertion about the drawing. Everything else in this file
// compares the two orientations against each other, which cannot see an error
// that is symmetric in both — and a symmetric one is the likely error, because
// alongRun and acrossRun are two branches of the same thought. Inverting either
// pair leaves the transpose test green while every edge in the DEFAULT top-down
// view comes apart.
//
// It is also the only thing that pins the junction table, which graph.go's own
// doc comment calls "the thing worth porting out of a charting library" and
// which had not one entry asserted anywhere.
func TestDrawChannelDrawsTheChannelItWasGiven(t *testing.T) {
	_, routes, depth := handChannel()
	if depth != 4 {
		t.Fatalf("channel depth %d, want 4 (a stub, two buses and the arrowheads)", depth)
	}

	td := []string{
		"  ╷       ╷  ",
		"  ╰───╮   │  ",
		"      ├───╯  ",
		"      ▼      ",
	}
	if got := drawChannel(orientTopDown, 13, routes, depth).rows(); !equalRows(got, td) {
		t.Errorf("top-down channel:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(td, "\n"))
	}

	// The same channel a quarter turn round: every run swaps axis, every corner
	// glyph rotates with it, and the arrowhead points the way the layers run.
	lr := []string{
		"    ",
		"    ",
		"─╮  ",
		" │  ",
		" │  ",
		" │  ",
		" ╰┬▶",
		"  │ ",
		"  │ ",
		"  │ ",
		"──╯ ",
		"    ",
		"    ",
	}
	if got := drawChannel(orientLeftRight, 13, routes, depth).rows(); !equalRows(got, lr) {
		t.Errorf("left-right channel:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(lr, "\n"))
	}
}

// A channel with ONE bussed edge, where nothing else writes the cell the run
// ends on. In the two-source case above every interesting cell is written twice
// and the arrowhead's explicit glyph covers the last row, so a run that
// overshoots by one is invisible there — this is the shape that sees it.
func TestASingleBussedEdgeEndsItsRunWhereTheArrowheadBegins(t *testing.T) {
	l := &egoLayout{Nodes: map[string]*egoNode{
		"a": {Key: "a", Kind: egoReal, Rank: 0, Along: 0, Span: 4},
		"b": {Key: "b", Kind: egoReal, Rank: 1, Along: 8, Span: 4},
	}}
	l.Edges = []egoEdge{{From: "a", To: "b"}}
	routes, depth := routeChannel(l, l.Edges)
	if depth != 3 {
		t.Fatalf("channel depth %d, want 3 (a stub, one bus and the arrowheads)", depth)
	}
	want := []string{
		"  ╷        ",
		"  ╰───────┤",
		"          ▼",
	}
	if got := drawChannel(orientTopDown, 11, routes, depth).rows(); !equalRows(got, want) {
		t.Errorf("channel:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func equalRows(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

// The pass-through has to be drawn ON the row its channel attaches to, and to
// span the whole rank it is travelling through. Asserting the layout numbers
// alone was self-referential — it compared place()'s output against the
// function that produced it and never looked at a drawing.
func TestAPassThroughIsDrawnAsOneFullWidthRule(t *testing.T) {
	m := New(memstore.NewWith(spanningBoard()), Options{GraphLR: true})
	m.selectID("t-c", false)
	m.openGraph()
	m.graphRadius = graphAllRadius
	if _, err := m.Dump(240, 50, "", true); err != nil {
		t.Fatal(err)
	}
	l := m.graphLay
	f := m.graphMeasure(l)

	var dummies int
	for r := range l.Layers {
		col := m.graphRankColumn(l.Layers[r], f, maxInt(m.graphCanvasH(), l.Extent()))
		for _, n := range l.Layers[r] {
			if n.Kind != egoDummy {
				continue
			}
			dummies++
			rule := strings.Repeat("─", f.nodeW)
			if got := ansiStrip(col[n.Along]); got != rule {
				t.Errorf("the pass-through row is %q, want %d cells of rule", got, f.nodeW)
			}
			for _, off := range []int{-1, 1} {
				y := n.Along + off
				if y < 0 || y >= len(col) {
					continue
				}
				if got := strings.TrimSpace(ansiStrip(col[y])); got != "" {
					t.Errorf("the row %+d from the pass-through is %q, want blank — the rule "+
						"must sit on the row the channel attaches to", off, got)
				}
			}
		}
	}
	if dummies == 0 {
		t.Fatal("no dummy in the spanning fixture, so nothing was checked")
	}
}

// The shed ORDER is a claim the comment, the glossary and this test have to
// agree on. Asserting that "kyushu-trip" appears somewhere in the canvas said
// nothing: every task on the chain carries it, so any unsqueezed box satisfied
// it while the box that actually shed something went unexamined.
func TestTheIDLineShedsItsExtrasInTheDocumentedOrder(t *testing.T) {
	m, _ := chainGraph(t, 12, 240, 50)
	l := m.graphLay
	f := m.graphMeasure(l)
	focus := l.FocusNode()
	if focus == nil {
		t.Fatal("no focus node")
	}

	tight := ansiStrip(strings.Split(m.renderGraphNode(focus, f.nodeW, f.titleLines), "\n")[1])
	if !strings.Contains(tight, focus.ID) {
		t.Errorf("the focus box lost its id at the floor width: %q", tight)
	}
	if !strings.Contains(tight, "kyushu-trip") {
		t.Errorf("the focus box shed the repo chip before the focus badge: %q", tight)
	}
	if strings.Contains(tight, "◉ focus") {
		t.Errorf("the focus badge survived at a width where the chip had to go: %q", tight)
	}

	// Given room, everything comes back — the shedding is a squeeze, not a rule.
	wide := ansiStrip(strings.Split(m.renderGraphNode(focus, graphNodeMaxW, f.titleLines), "\n")[1])
	for _, want := range []string{focus.ID, "kyushu-trip", "◉ focus"} {
		if !strings.Contains(wide, want) {
			t.Errorf("a %d-cell box dropped %q: %q", graphNodeMaxW, want, wide)
		}
	}
}

// The notes go BESIDE the lone box, on its own centre row. Searching the whole
// frame for the words could not tell that from putting them on its bottom
// border, or off the right edge entirely.
func TestTheIsolatedFocusNotesSitBesideTheBox(t *testing.T) {
	b := board.NewBoard([]*board.Task{
		{ID: "t-lonely", Title: "誰も待たず誰にも待たれない作業", Status: "backlog", Priority: 10},
	})
	for _, w := range []int{160, 240, 400} {
		m := New(memstore.NewWith(b), Options{GraphLR: true})
		m.selectID("t-lonely", false)
		m.openGraph()
		out, err := m.Dump(w, 50, "", true)
		if err != nil {
			t.Fatalf("w=%d: %v", w, err)
		}
		canvas := strings.Split(canvasOf(m, out), "\n")

		row := -1
		for i, line := range canvas {
			if strings.Contains(line, graphNoUpstream) {
				row = i
			}
		}
		if row < 0 {
			t.Errorf("w=%d: %q is not in the drawing", w, graphNoUpstream)
			continue
		}
		if !strings.Contains(canvas[row], graphNoStructure) {
			t.Errorf("w=%d: the second note is not on the same row as the first: %q", w, canvas[row])
		}
		n := m.graphLay.FocusNode()
		if n == nil {
			t.Fatalf("w=%d: no focus node", w)
		}
		// The box's own centre row, which is a title row rather than the id
		// line — the point is that the notes are level with the box, not that
		// they are level with its name.
		if row != n.Anchor() {
			t.Errorf("w=%d: the notes are on row %d, the box's centre is %d", w, row, n.Anchor())
		}
		if row < n.Along || row >= n.Along+n.Span {
			t.Errorf("w=%d: row %d is outside the box's rows %d..%d", w, row, n.Along, n.Along+n.Span-1)
		}
	}
}

// The left-right window grows from the SELECTION, so walking across the layers
// pulls the frame along instead of leaving the cursor on a box nobody drew.
func TestTheLeftRightWindowFollowsTheSelection(t *testing.T) {
	m, _ := chainGraph(t, 12, 240, 50)
	for i := 0; i < 8; i++ {
		m.graphMove(+1, 0) // left-right: the horizontal pair crosses layers
		out, err := m.Dump(240, 50, "", true)
		if err != nil {
			t.Fatal(err)
		}
		n := m.graphLay.Node(m.graphSel)
		if n == nil {
			t.Fatalf("step %d: the selection left the layout", i)
		}
		f := m.graphMeasure(m.graphLay)
		if n.Rank < f.first || n.Rank > f.last {
			t.Fatalf("step %d: selection %s is on rank %d, outside the drawn window %d..%d",
				i, n.ID, n.Rank, f.first, f.last)
		}
		if !strings.Contains(canvasOf(m, out), n.ID) {
			t.Fatalf("step %d: selection %s is not drawn anywhere in the canvas", i, n.ID)
		}
		// Eight different windows, so this is where a window one cell too
		// greedy actually shows up.
		fitsTheWidth(t, m, f)
	}
}

// fanOutBoard is one task blocking n others, so its ego graph has a layer wider
// than graphHardCols. The fixture's widest is 3, which is why neither the cap
// nor the over-width report has a route through it.
func fanOutBoard(n int) *board.Board {
	tasks := []*board.Task{{ID: "t-hub", Title: "扇の要 — 枝を一手に塞ぐ", Status: "backlog", Priority: 10}}
	for i := 0; i < n; i++ {
		tasks = append(tasks, &board.Task{
			ID:       fmt.Sprintf("t-f%02d", i),
			Title:    fmt.Sprintf("枝の%d本目 — 要が空くまで動けない", i),
			Status:   "backlog",
			Priority: (i + 2) * 10,
			Deps:     []string{"t-hub"},
		})
	}
	return board.NewBoard(tasks)
}

// The row cap has a report and no way to see it: the fixture's widest layer is
// 3 and the cap is 6. On main a terminal narrower than the design floor lowered
// the cap and made it fire; that route went with the width-derived cap, which
// had to go so `o` could not change the node count. A fan-out board reaches it
// on its own terms, and in both orientations.
func TestALayerWiderThanTheCapIsReportedInBothOrientations(t *testing.T) {
	b := fanOutBoard(8)
	for _, lr := range []bool{false, true} {
		m := New(memstore.NewWith(b), Options{GraphLR: lr})
		m.selectID("t-hub", false)
		m.openGraph()
		out, err := m.Dump(240, 50, "", true)
		if err != nil {
			t.Fatalf("lr=%v: %v", lr, err)
		}
		if len(m.graphLay.Overflow) == 0 {
			t.Fatalf("lr=%v: an 8-wide layer did not overflow a cap of %d", lr, graphHardCols)
		}
		if !strings.Contains(out, "over the layer cap") {
			t.Errorf("lr=%v: nodes were dropped and the header never said so", lr)
		}
	}
}

// Top-down has no horizontal scroll, so a layer wider than the drawing is cut
// by renderGraph's pad. Below the 240-column floor that is reachable — six
// boxes at the minimum width need 183 cells — and this file's own rule is that
// a graph which quietly omits a node is worse than one that admits it.
func TestATopDownLayerWiderThanTheScreenSaysSo(t *testing.T) {
	b := fanOutBoard(8)
	for _, tc := range []struct {
		w    int
		says bool
	}{
		{160, true},  // six boxes at the floor width do not fit
		{240, false}, // the design floor: they do
	} {
		m := New(memstore.NewWith(b), Options{})
		m.selectID("t-hub", false)
		m.openGraph()
		out, err := m.Dump(tc.w, 50, "", true)
		if err != nil {
			t.Fatalf("w=%d: %v", tc.w, err)
		}
		if got := strings.Contains(out, "past the right edge"); got != tc.says {
			t.Errorf("w=%d: reports over-width=%v, want %v", tc.w, got, tc.says)
		}
	}
}

// The rank window has to fit the drawing at EVERY width, not just the ones a
// demo happens to use: it is a greedy loop over per-rank costs, so it goes
// wrong one cell at a time and only at the widths where a rank lands on the
// boundary.
func TestTheRankWindowFitsTheDrawingAtEveryWidth(t *testing.T) {
	for w := 200; w <= 340; w++ {
		m := New(memstore.NewWith(chainBoard(12)), Options{GraphLR: true})
		m.selectID("t-c06", false)
		m.openGraph()
		m.graphRadius = graphAllRadius
		out, err := m.Dump(w, 50, "", true)
		if err != nil {
			t.Fatalf("w=%d: %v", w, err)
		}
		f := m.graphMeasure(m.graphLay)
		total := (f.last - f.first + 1) * f.nodeW
		for r := f.first; r < f.last; r++ {
			total += f.channels[r]
		}
		if total > m.graphWidth() {
			t.Errorf("w=%d: the bands add up to %d in a %d-cell drawing", w, total, m.graphWidth())
		}
		for _, line := range strings.Split(out, "\n") {
			if got := lg.Width(line); got != w {
				t.Fatalf("w=%d: a row is %d cells wide", w, got)
			}
		}
	}
}

// A canvas shorter than one box must scroll, not hand out a slot smaller than
// the box drawn in it — which fused the borders and put every anchor on one.
func TestAShortCanvasNeverPlacesABoxSmallerThanItDraws(t *testing.T) {
	for h := 4; h <= 20; h++ {
		m := New(memstore.New(), Options{GraphLR: true})
		if _, err := m.Dump(240, h, "graphall", true); err != nil {
			t.Fatalf("rows=%d: %v", h, err)
		}
		f := m.graphMeasure(m.graphLay)
		if got, want := f.nodeH(), m.graphLay.Span; got != want {
			t.Errorf("rows=%d: boxes are drawn %d rows tall in a %d-row slot", h, got, want)
		}
	}
}

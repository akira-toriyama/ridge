package ui

import (
	"fmt"
	"github.com/akira-toriyama/ridge/internal/board"
	"sort"
)

// The dependency GRAPH view's layout engine — pure, deterministic, and with no
// knowledge of lipgloss, the theme or the terminal. graphview.go draws what
// this file decides.
//
// The shape is an EGO GRAPH around one focus task: "what must finish before
// this" on one side of it, "what closing this unblocks" on the other. Direction
// is carried by POSITION and, redundantly, by arrowheads — a reader must never
// have to remember which way the arrows point.
//
// WHICH SCREEN AXIS the layers run along is a setting, so nothing in this file
// says "row" or "column". It has two axes instead:
//
//	ALONG  — inside one layer, from one sibling to the next
//	ACROSS — from a layer to the next layer out
//
// orientTopDown maps along→x and across→y; orientLeftRight maps along→y and
// across→x. Every distance below is on one of those two axes, so the engine is
// written once and the drawing decides what they mean on screen.
//
// The algorithm is Sugiyama PHASE 1 ONLY (longest-path layering) plus a single
// barycenter sweep. That is a deliberate stopping point, not laziness: measured
// over the real 658-task board, naive id-order placement produces **2** edge
// crossings across ALL 131 ego-graphs, so a real crossing-reduction pass would
// be several hundred lines buying two glyphs. The barycenter sweep is ~20 lines
// and is kept because it is nearly free and makes long fan-outs read straight.
//
// Everything here is bounded and cycle-safe. The real board has no cycles, but
// a git merge can produce one and a graph view that hangs is worse than one
// that draws a cycle awkwardly.

// graphOrient is which screen axis the LAYERS run along.
//
// Both values keep the redundancy contract above: top-down puts upstream ABOVE
// and points every arrow DOWN, left-right puts upstream LEFT and points every
// arrow RIGHT. Position and arrowhead always say the same thing.
type graphOrient int

const (
	orientTopDown graphOrient = iota
	orientLeftRight
)

// String is the word the header and the status line use, so the two surfaces
// cannot drift apart.
func (o graphOrient) String() string {
	if o == orientLeftRight {
		return "left-right"
	}
	return "top-down"
}

const (
	// graphAllRadius is the "all" setting of the hop-radius cycle. The real
	// board's longest chain is 5 edges, so 8 reaches everything while still
	// bounding every walk in this file.
	graphAllRadius = 8

	// graphHardCols caps how many nodes one layer may DRAW. The measured
	// widest layer over the whole board is 4; the cap exists so a pathological
	// fan-out degrades into "+N more" instead of into an unreadable frame.
	//
	// It is a property of the PICTURE, not of the terminal, and deliberately
	// not derived from either screen axis: a cap read off the width and a cap
	// read off the height would disagree, and `o` would then change how many
	// nodes the board has.
	graphHardCols = 6

	// The along-axis extents, top-down: screen columns.
	graphNodeMinW = 28
	graphNodeMaxW = 104
	graphNodeGap  = 3
	graphDummyW   = 3

	// The along-axis extents, left-right: screen ROWS. A node box costs
	// border(2) + the id line + the meta line on top of its title lines, so its
	// height is the title budget (graphview.go) plus that fixed chrome. The gap
	// is 1 row against top-down's 3 columns because a terminal cell is about
	// twice as tall as it is wide, and a routing dummy is a single rule.
	graphNodeChrome = 4
	graphNodeMinH   = graphMinNodeLines + graphNodeChrome
	graphNodeMaxH   = graphMaxNodeLines + graphNodeChrome
	graphNodeGapLR  = 1
	graphDummyLR    = 1

	// graphNodeMinWLR is how narrow a node box may get in the left-right frame,
	// where WIDTH is the negotiated axis rather than the given one.
	//
	// It is the width at which the box still says something. 36 outer is 32 of
	// inner width — 16 Japanese glyphs a line, so the three title lines the
	// frame affords clear the 82-cell median title (CLAUDE.md) with room, and
	// the id line still fits its repo chip beside the lane. At 28, top-down's
	// floor, the same box holds 72 cells of title and sheds the chip.
	//
	// Below it the picture is boxes you cannot read, so graphview.go drops
	// LAYERS and says so instead of going under it. The id itself survives much
	// further down — renderGraphNode sheds the optional halves of the id line
	// to keep it — which is what makes this a legibility floor and not a
	// correctness one.
	graphNodeMinWLR = 36
)

// nodeSpans is the along-axis budget for one node: the extent a real node may
// take, the gap between two of them, and what a routing dummy costs. Top-down
// measures all three in screen columns, left-right in screen rows.
func nodeSpans(o graphOrient) (lo, hi, gap, dummy int) {
	if o == orientLeftRight {
		return graphNodeMinH, graphNodeMaxH, graphNodeGapLR, graphDummyLR
	}
	return graphNodeMinW, graphNodeMaxW, graphNodeGap, graphDummyW
}

// egoKind separates real tasks from the routing dummies that carry an edge
// spanning more than one layer through the layers in between.
type egoKind int

const (
	egoReal egoKind = iota
	egoDummy
)

// egoNode is one placed node. Dummies get a synthetic Key and no Task.
type egoNode struct {
	Key   string // node identity in the layout (a task id, or a dummy key)
	ID    string // the task id; "" for a dummy
	Kind  egoKind
	Layer int // SIGNED: negative = upstream, 0 = focus, positive = downstream
	Rank  int // index into egoLayout.Layers (0 = the outermost upstream layer)
	Slot  int // position within the layer, in along-axis order

	Focus   bool // the task the graph is rooted on
	Both    bool // reachable both upstream AND downstream (only possible in a cycle)
	Hidden  bool // the current board filter would hide this task
	Unknown bool // a dep pointing at an id that is not on the board

	// Filled by place(). Along is the node's offset on the ALONG axis and Span
	// is its extent there. The extent on the ACROSS axis is uniform across the
	// whole frame and is negotiated by the view, which is why it is not here.
	Along, Span int
}

// Anchor is the along-axis position an edge attaches to: the node's centre.
func (n *egoNode) Anchor() int { return n.Along + n.Span/2 }

// egoEdge is one drawn edge, always between ADJACENT ranks after dummy
// insertion, always pointing OUTWARD from the upstream side (From is the lower
// rank, drawn above the focus top-down and left of it left-right).
type egoEdge struct{ From, To string }

// egoLayout is one frame's worth of graph structure.
type egoLayout struct {
	Focus  string
	Radius int

	Nodes  map[string]*egoNode
	Layers [][]*egoNode // index 0 = the outermost upstream layer
	Edges  []egoEdge

	// Skipped are real dep edges the layered drawing cannot express: a cycle
	// folded two nodes onto the same rank, or an edge pointing back upstream.
	// They are reported in the UI rather than silently dropped — a graph that
	// quietly omits an edge is worse than one that admits it.
	Skipped []egoEdge

	// Overflow counts nodes dropped from a layer by graphHardCols, by rank.
	Overflow map[int]int

	UpCount, DownCount int

	// Along is the along-axis extent place() laid this frame out in, and Span
	// is the extent it gave every real node on that axis.
	Along, Span int
}

// FocusNode is the node the graph is rooted on.
func (l *egoLayout) FocusNode() *egoNode { return l.Nodes[l.Focus] }

// Real lists every real (non-dummy) node in draw order: rank, then slot.
func (l *egoLayout) Real() []*egoNode {
	var out []*egoNode
	for _, row := range l.Layers {
		for _, n := range row {
			if n.Kind == egoReal {
				out = append(out, n)
			}
		}
	}
	return out
}

func (l *egoLayout) Node(key string) *egoNode { return l.Nodes[key] }

// Empty reports a focus with no dependency structure at all in either
// direction. The view says so in words rather than drawing a lone box in the
// middle of an empty screen and leaving the reader to wonder what broke.
func (l *egoLayout) Empty() bool { return l.UpCount == 0 && l.DownCount == 0 }

// FocusRank is the rank the focus sits on, and 0 when the focus is somehow
// absent — the value the layer window grows outward from.
func (l *egoLayout) FocusRank() int {
	if n := l.FocusNode(); n != nil {
		return n.Rank
	}
	return 0
}

// longestDist is bounded longest-path layering from `from` over the edges
// `next` yields. Longest path — not shortest — is Sugiyama's phase 1: it is
// what guarantees every edge points strictly outward, which is what lets the
// channel router assume a direction.
//
// It is a Bellman-Ford-shaped relaxation rather than a DFS precisely so a cycle
// cannot recurse forever: distances only ever increase, they are capped at
// radius by the expansion guard, and the outer loop runs at most radius times.
// A cycle simply saturates. Iteration order is over SORTED keys, so the same
// board always produces the same layering.
func longestDist(next func(string) []string, from string, radius int) map[string]int {
	dist := map[string]int{from: 0}
	for round := 0; round < radius; round++ {
		keys := make([]string, 0, len(dist))
		for k := range dist {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		changed := false
		for _, u := range keys {
			du := dist[u]
			if du >= radius {
				continue
			}
			for _, v := range next(u) {
				if v == from {
					continue // the focus owns layer 0; never re-layer it
				}
				if old, ok := dist[v]; !ok || du+1 > old {
					dist[v] = du + 1
					changed = true
				}
			}
		}
		if !changed {
			break
		}
	}
	delete(dist, from)
	return dist
}

// buildEgo lays out the dependency structure around `focus`.
//
// hidden reports whether the current board filter would hide a task. The graph
// draws it ANYWAY — the graph is about dependency structure, not about the
// active query, and an edge that vanishes because of a filter is a lie about
// the board. Such nodes are MARKED, never dropped.
func buildEgo(g *board.Graph, focus string, radius, maxCols int, hidden func(string) bool) *egoLayout {
	if radius < 1 {
		radius = 1
	}
	if maxCols < 1 {
		maxCols = 1
	}
	if maxCols > graphHardCols {
		maxCols = graphHardCols
	}

	l := &egoLayout{
		Focus:    focus,
		Radius:   radius,
		Nodes:    map[string]*egoNode{},
		Overflow: map[int]int{},
	}

	depsOf := func(id string) []string {
		t := g.Board().Task(id)
		if t == nil {
			return nil
		}
		return t.Deps
	}

	up := longestDist(depsOf, focus, radius)
	down := longestDist(g.Blocks, focus, radius)
	l.UpCount, l.DownCount = len(up), len(down)

	// --- 1. layer assignment ------------------------------------------------
	add := func(id string, layer int, both bool) {
		n := &egoNode{Key: id, ID: id, Kind: egoReal, Layer: layer, Both: both}
		n.Focus = id == focus
		n.Unknown = !g.Known(id)
		if hidden != nil {
			n.Hidden = hidden(id)
		}
		l.Nodes[id] = n
	}
	add(focus, 0, false)

	ids := make([]string, 0, len(up)+len(down))
	for id := range up {
		ids = append(ids, id)
	}
	for id := range down {
		if _, ok := up[id]; !ok {
			ids = append(ids, id)
		}
	}
	sort.Strings(ids)
	for _, id := range ids {
		du, okUp := up[id]
		dd, okDown := down[id]
		switch {
		case okUp && okDown:
			// Only reachable in a cycle. Place it on the side that puts it
			// CLOSER to the focus, upstream winning ties, and flag it — the
			// node genuinely occupies both roles and the strip says so.
			if du <= dd {
				add(id, -du, true)
			} else {
				add(id, dd, true)
			}
		case okUp:
			add(id, -du, false)
		default:
			add(id, dd, false)
		}
	}

	// --- 2. group into ranks, cap the width ---------------------------------
	byLayer := map[int][]*egoNode{}
	for _, n := range l.Nodes {
		byLayer[n.Layer] = append(byLayer[n.Layer], n)
	}
	layerVals := make([]int, 0, len(byLayer))
	for v := range byLayer {
		layerVals = append(layerVals, v)
	}
	sort.Ints(layerVals)

	for i, v := range layerVals {
		row := byLayer[v]
		sort.Slice(row, func(a, b int) bool { return row[a].Key < row[b].Key })
		if len(row) > maxCols {
			// The focus must survive its own graph, whatever the id ordering.
			if v == 0 {
				for j, n := range row {
					if n.Focus && j >= maxCols {
						row[maxCols-1], row[j] = row[j], row[maxCols-1]
						break
					}
				}
			}
			for _, n := range row[maxCols:] {
				delete(l.Nodes, n.Key)
			}
			l.Overflow[i] = len(row) - maxCols
			row = row[:maxCols]
		}
		for _, n := range row {
			n.Rank = i
		}
		l.Layers = append(l.Layers, row)
	}

	// --- 3. edges over the INDUCED subgraph ---------------------------------
	// Every dep edge whose BOTH ends survived, plus dummies for spans > 1.
	type raw struct{ from, to string } // from = upstream (the dependency)
	var raws []raw
	seen := map[raw]bool{}
	for _, n := range l.Real() {
		for _, dep := range depsOf(n.ID) {
			if _, ok := l.Nodes[dep]; !ok {
				continue
			}
			r := raw{from: dep, to: n.ID}
			if seen[r] {
				continue
			}
			seen[r] = true
			raws = append(raws, r)
		}
	}
	sort.Slice(raws, func(a, b int) bool {
		if raws[a].from != raws[b].from {
			return raws[a].from < raws[b].from
		}
		return raws[a].to < raws[b].to
	})

	for _, r := range raws {
		fr, to := l.Nodes[r.from], l.Nodes[r.to]
		span := to.Rank - fr.Rank
		if span <= 0 {
			// A cycle folded the edge flat or backwards. Report it; do not try
			// to route it, and above all do not loop looking for a way.
			l.Skipped = append(l.Skipped, egoEdge{From: r.from, To: r.to})
			continue
		}
		if span == 1 {
			l.Edges = append(l.Edges, egoEdge{From: r.from, To: r.to})
			continue
		}
		// span > 1: chain dummies through the ranks in between, so the channel
		// router only ever sees adjacent-rank edges.
		prev := r.from
		for rank := fr.Rank + 1; rank < to.Rank; rank++ {
			key := fmt.Sprintf("\x00dummy:%s>%s@%d", r.from, r.to, rank)
			d := &egoNode{Key: key, Kind: egoDummy, Layer: layerVals[rank], Rank: rank}
			l.Nodes[key] = d
			l.Layers[rank] = append(l.Layers[rank], d)
			l.Edges = append(l.Edges, egoEdge{From: prev, To: key})
			prev = key
		}
		l.Edges = append(l.Edges, egoEdge{From: prev, To: r.to})
	}

	l.orderRanks()
	return l
}

// orderRanks is the single barycenter sweep, run OUTWARD from the focus rank so
// that every rank is ordered against one whose slots are already fixed.
//
// Sorting is stable and the tie-break is the node key, so a node with no
// neighbours in the reference rank keeps its id-order position instead of
// floating.
func (l *egoLayout) orderRanks() {
	focusRank := l.FocusRank()

	adj := map[string][]string{}
	for _, e := range l.Edges {
		adj[e.From] = append(adj[e.From], e.To)
		adj[e.To] = append(adj[e.To], e.From)
	}

	slotIn := func(rank int) map[string]int {
		m := map[string]int{}
		for i, n := range l.Layers[rank] {
			m[n.Key] = i
		}
		return m
	}

	sweep := func(rank, ref int) {
		refSlots := slotIn(ref)
		bary := map[string]float64{}
		for _, n := range l.Layers[rank] {
			sum, cnt := 0.0, 0
			for _, nb := range adj[n.Key] {
				if s, ok := refSlots[nb]; ok {
					sum += float64(s)
					cnt++
				}
			}
			if cnt == 0 {
				bary[n.Key] = -1 // no opinion: keep the incoming order
			} else {
				bary[n.Key] = sum / float64(cnt)
			}
		}
		sort.SliceStable(l.Layers[rank], func(a, b int) bool {
			na, nb := l.Layers[rank][a], l.Layers[rank][b]
			ba, bb := bary[na.Key], bary[nb.Key]
			if ba < 0 || bb < 0 {
				return false // stable: leave opinionless nodes where they are
			}
			if ba != bb {
				return ba < bb
			}
			return na.Key < nb.Key
		})
	}

	for r := focusRank - 1; r >= 0; r-- {
		sweep(r, r+1)
	}
	for r := focusRank + 1; r < len(l.Layers); r++ {
		sweep(r, r-1)
	}
	for _, row := range l.Layers {
		for i, n := range row {
			n.Slot = i
		}
	}
}

// place assigns every node an offset and an extent on the ALONG axis, inside a
// canvas `avail` long — screen columns top-down, screen rows left-right.
//
// Real nodes all get the SAME extent — the grid reads as a grid — sized so the
// busiest layer fits. Dummies get a routing artefact's worth, because a
// pass-through should not cost a whole box. Each layer is then CENTRED, which
// is what puts the focus box in the middle of the frame with its fan-out spread
// symmetrically around it.
func (l *egoLayout) place(o graphOrient, avail int) {
	if avail < 1 {
		avail = 1
	}
	cols := 1
	for _, row := range l.Layers {
		n := 0
		for _, nd := range row {
			if nd.Kind == egoReal {
				n++
			}
		}
		if n > cols {
			cols = n
		}
	}

	lo, hi, gap, dummy := nodeSpans(o)
	span := (avail - (cols-1)*gap) / cols
	span = clamp(span, lo, hi)
	if o == orientTopDown && span > avail {
		// A box wider than the whole canvas is meaningless, so top-down
		// shrinks it. Left-right must NOT: the box's extent on this axis is
		// its HEIGHT, which the view floors at graphNodeMinH, so a smaller
		// Span would hand out a slot smaller than the box drawn in it —
		// boxes overlapping, every anchor landing on a border, and rows the
		// canvas never sizes for. The along axis overflows into the scroll
		// instead, which is the whole reason that scroll survived the change.
		span = maxInt(1, avail)
	}

	for _, row := range l.Layers {
		total := 0
		for i, n := range row {
			n.Span = span
			if n.Kind == egoDummy {
				n.Span = dummy
			}
			if i > 0 {
				total += gap
			}
			total += n.Span
		}
		x := maxInt(0, (avail-total)/2)
		for _, n := range row {
			n.Along = x
			x += n.Span + gap
		}
	}
	l.Along, l.Span = avail, span
}

// Extent is how far along the axis the longest layer actually reaches. place()
// centres each layer inside `avail`, so a layer that does not fit starts at 0
// and runs past it — the view sizes its canvas from this rather than assuming
// the content fits.
func (l *egoLayout) Extent() int {
	out := 0
	for _, row := range l.Layers {
		for _, n := range row {
			if end := n.Along + n.Span; end > out {
				out = end
			}
		}
	}
	return out
}

// rankEdges returns the edges that live in the channel between rank r and r+1.
func (l *egoLayout) rankEdges(r int) []egoEdge {
	var out []egoEdge
	for _, e := range l.Edges {
		if l.Nodes[e.From].Rank == r && l.Nodes[e.To].Rank == r+1 {
			out = append(out, e)
		}
	}
	return out
}

// Direction bits. A cell records which of its four sides a line leaves by, and
// the rune is then LOOKED UP from that mask. This is the whole junction-merge
// table — the thing worth porting out of a charting library — expressed as the
// 16-entry array it always was: two lines meeting in one cell can only produce
// the glyph their combined mask names, so `─` crossing `│` cannot come out as
// anything but `┼`, and a tee cannot come out as a corner.
//
// The table is also what makes the two orientations one piece of code. A
// channel is described as runs along and across its own axes, and because the
// bits are COMPASS directions rather than "the bus direction", the same run
// comes out as `│` on one axis and `─` on the other with no second table.
const (
	dirN uint8 = 1 << iota
	dirE
	dirS
	dirW
)

var junction = [16]rune{
	0:                         ' ',
	dirN:                      '╵',
	dirE:                      '─',
	dirN | dirE:               '╰',
	dirS:                      '╷',
	dirN | dirS:               '│',
	dirE | dirS:               '╭',
	dirN | dirE | dirS:        '├',
	dirW:                      '─',
	dirN | dirW:               '╯',
	dirE | dirW:               '─',
	dirN | dirE | dirW:        '┴',
	dirS | dirW:               '╮',
	dirN | dirS | dirW:        '┤',
	dirE | dirS | dirW:        '┬',
	dirN | dirE | dirS | dirW: '┼',
}

// edgeCanvas is a plain rune grid, and it is only ever used for the CHANNEL
// bands between layers — never for anything containing text.
//
// That restriction is the whole reason it is safe. A rune-per-cell grid has no
// idea that a Japanese glyph is two cells wide, so the moment one is written
// into it every row to the right shears. (That is exactly the defect that
// disqualified ntcharts' canvas for this job.) Node boxes therefore go through
// lipgloss, which measures display width, and only box-drawing characters —
// every one of them single-width, asserted by
// TestEdgeCanvasGlyphsAreSingleWidth — ever reach this type.
type edgeCanvas struct {
	w, h  int
	mask  []uint8
	glyph []rune // an explicit glyph wins over the junction table
}

func newEdgeCanvas(w, h int) *edgeCanvas {
	w, h = maxInt(w, 1), maxInt(h, 0)
	return &edgeCanvas{w: w, h: h, mask: make([]uint8, w*h), glyph: make([]rune, w*h)}
}

func (c *edgeCanvas) in(x, y int) bool { return x >= 0 && x < c.w && y >= 0 && y < c.h }

func (c *edgeCanvas) or(x, y int, bits uint8) {
	if c.in(x, y) {
		c.mask[y*c.w+x] |= bits
	}
}

func (c *edgeCanvas) put(x, y int, r rune) {
	if c.in(x, y) {
		c.glyph[y*c.w+x] = r
	}
}

// vline draws a vertical run at x from y0 to y1 inclusive.
func (c *edgeCanvas) vline(x, y0, y1 int) {
	if y1 < y0 {
		y0, y1 = y1, y0
	}
	for y := y0; y <= y1; y++ {
		var b uint8
		if y > y0 {
			b |= dirN
		}
		if y < y1 {
			b |= dirS
		}
		if y0 == y1 {
			b = dirN | dirS
		}
		c.or(x, y, b)
	}
}

// hline draws a horizontal run at y from x0 to x1 inclusive.
func (c *edgeCanvas) hline(y, x0, x1 int) {
	if x1 < x0 {
		x0, x1 = x1, x0
	}
	for x := x0; x <= x1; x++ {
		var b uint8
		if x > x0 {
			b |= dirW
		}
		if x < x1 {
			b |= dirE
		}
		if x0 == x1 {
			b = dirE | dirW
		}
		c.or(x, y, b)
	}
}

// The three axis-mapped writers. A channel is described entirely as runs ALONG
// its layer and runs ACROSS to the next one; these map that description onto
// the grid, and the compass-bit junction table above turns it into the right
// glyph on either axis without a second table.
func (c *edgeCanvas) alongRun(o graphOrient, across, a0, a1 int) {
	if o == orientLeftRight {
		c.vline(across, a0, a1)
		return
	}
	c.hline(across, a0, a1)
}

func (c *edgeCanvas) acrossRun(o graphOrient, along, c0, c1 int) {
	if o == orientLeftRight {
		c.hline(along, c0, c1)
		return
	}
	c.vline(along, c0, c1)
}

func (c *edgeCanvas) putAt(o graphOrient, along, across int, r rune) {
	if o == orientLeftRight {
		c.put(across, along, r)
		return
	}
	c.put(along, across, r)
}

// rows renders the canvas to strings — h of them, w cells each. Top-down that
// is one screen line per row; left-right it is one COLUMN band, which the view
// splices between two layers.
func (c *edgeCanvas) rows() []string {
	out := make([]string, c.h)
	buf := make([]rune, c.w)
	for y := 0; y < c.h; y++ {
		for x := 0; x < c.w; x++ {
			if g := c.glyph[y*c.w+x]; g != 0 {
				buf[x] = g
				continue
			}
			buf[x] = junction[c.mask[y*c.w+x]&0xf]
		}
		out[y] = string(buf)
	}
	return out
}

// routedEdge is one edge with its assigned bus inside a channel. A1 and A2 are
// its two ends' ALONG-axis anchors; Bus is the ACROSS-axis offset of the shared
// run between them.
type routedEdge struct {
	Edge    egoEdge
	A1, A2  int
	Bus     int  // -1 = a straight shot, no run along the layer at all
	ToDummy bool // no arrowhead: the line continues through a pass-through
}

// routeChannel packs the edges between two ranks into as few buses as it can,
// and returns the channel's depth on the ACROSS axis.
//
// The ordering rule is the one that makes an orthogonal channel readable:
// **shortest along-axis span nearest the source layer.** Long edges then arc
// AROUND short ones instead of cutting across them. Combined with the measured
// crossing count (2 across the entire real board) this is all the
// crossing-avoidance the drawing needs.
//
// Buses are shared by interval packing: two edges whose along-axis runs do not
// overlap sit on the same bus, so the common fan-out — several short hops —
// costs one bus rather than one each.
//
// It takes no orientation and needs none: the packing is topology, so one ego
// graph produces the same channel depths whichever way it is drawn. Padding the
// left-right channels for looks was tried and reverted — five channels at two
// cells each cost a whole LAYER at the 240-column floor, which is the one
// resource that frame is short of.
func routeChannel(l *egoLayout, edges []egoEdge) (routes []routedEdge, depth int) {
	type span struct{ lo, hi int }
	var busy [][]span

	for _, e := range edges {
		from, to := l.Nodes[e.From], l.Nodes[e.To]
		routes = append(routes, routedEdge{
			Edge: e, A1: from.Anchor(), A2: to.Anchor(),
			Bus: -1, ToDummy: to.Kind == egoDummy,
		})
	}
	sort.SliceStable(routes, func(a, b int) bool {
		sa := abs(routes[a].A2 - routes[a].A1)
		sb := abs(routes[b].A2 - routes[b].A1)
		if sa != sb {
			return sa < sb
		}
		if routes[a].A1 != routes[b].A1 {
			return routes[a].A1 < routes[b].A1
		}
		return routes[a].Edge.From < routes[b].Edge.From
	})

	for i := range routes {
		r := &routes[i]
		if r.A1 == r.A2 {
			continue // a straight shot: it needs no bus
		}
		lo, hi := minInt(r.A1, r.A2), maxInt(r.A1, r.A2)
		placed := false
		for b := range busy {
			free := true
			for _, s := range busy[b] {
				// A one-cell margin: two runs that merely TOUCH would fuse
				// into a single line and read as one edge. One cell on both
				// axes — unlike the node gaps, this is a topological minimum
				// rather than a visual one, so the cell aspect ratio does not
				// enter into it.
				if lo <= s.hi+1 && s.lo <= hi+1 {
					free = false
					break
				}
			}
			if free {
				busy[b] = append(busy[b], span{lo, hi})
				r.Bus = b
				placed = true
				break
			}
		}
		if !placed {
			busy = append(busy, []span{{lo, hi}})
			r.Bus = len(busy) - 1
		}
	}

	// Offset 0 is a clean stub leaving the source boxes, 1..n are the buses,
	// and the last offset carries the arrowheads against the target boxes.
	depth = len(busy) + 2
	if len(edges) == 0 {
		depth = 1
	}
	for i := range routes {
		if routes[i].Bus >= 0 {
			routes[i].Bus++
		}
	}
	return routes, depth
}

// drawChannel paints one channel's edges onto a fresh canvas `along` long and
// `depth` deep, in whichever screen orientation those two axes map to.
func drawChannel(o graphOrient, along int, routes []routedEdge, depth int) *edgeCanvas {
	w, h := along, depth
	if o == orientLeftRight {
		w, h = depth, along
	}
	c := newEdgeCanvas(w, h)
	tip := depth - 1
	for _, r := range routes {
		if r.Bus < 0 {
			c.acrossRun(o, r.A1, 0, tip-1)
		} else {
			c.acrossRun(o, r.A1, 0, r.Bus)
			c.alongRun(o, r.Bus, r.A1, r.A2)
			c.acrossRun(o, r.A2, r.Bus, tip-1)
		}
		if r.ToDummy {
			// A pass-through is not a destination; the line simply continues.
			c.acrossRun(o, r.A2, tip-1, tip)
		} else {
			c.putAt(o, r.A2, tip, graphArrow(o))
		}
	}
	return c
}

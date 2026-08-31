package ui

import (
	"fmt"
	"github.com/akira-toriyama/ridge/internal/board"
	"strings"

	lg "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The dependency graph VIEW: a full-screen, Obsidian-shaped picture of the
// structure around one task. graph.go decided the geometry; this file paints it.
//
// Two rules keep it honest at 240-400 columns:
//
//  1. NODE BOXES GO THROUGH LIPGLOSS, EDGES GO THROUGH THE RUNE GRID. Text is
//     measured in DISPLAY cells (a Japanese title is two cells per glyph);
//     box-drawing characters are all single-width and can therefore live in a
//     plain rune buffer. Mixing the two is the CJK shear bug, so the frame is
//     built as alternating BANDS — a lipgloss-composed rank of boxes, then a
//     rune-grid channel, then the next rank — and every band is exactly its
//     declared extent on every line, so the joins are width-exact. Top-down
//     stacks the bands as screen lines; left-right stacks them as columns and
//     joins them per line, the way mapBands already composes the dep map.
//
//  2. THE GIVEN AXIS IS SPENT, THE OTHER IS NEGOTIATED — and which is which
//     swaps with the orientation. Top-down is given the width: node widths fall
//     out of it and the title-line budget is whatever height the channels
//     leave. Left-right is given the height: the title-line budget falls out of
//     it and the node WIDTH is whatever width the channels leave. That is why
//     the same graph reads as 2 title lines at 240x60 and 3 at 400x90.

const (
	graphMinNodeLines = 1
	graphMaxNodeLines = 3
)

// What an isolated focus is told, spelled once because the left-right frame has
// to reserve their width before it can size a box. Top-down stacks them above
// and below the lone box; left-right sets them either side of it, where the
// structure they report missing would have been.
const (
	graphNoUpstream  = "— no blockers —"
	graphNoStructure = "— nothing depends on this, and it waits on nothing —"
)

// graphRadii is the hop-radius cycle. The last entry is "all" — bounded by
// graphAllRadius, which exceeds the real board's longest chain (5).
var graphRadii = []int{1, 2, 3, graphAllRadius}

func radiusLabel(r int) string {
	if r >= graphAllRadius {
		return "all"
	}
	return fmt.Sprintf("%d", r)
}

// graphCanvasH is how many rows the drawing itself may use.
func (m *Model) graphCanvasH() int {
	return maxInt(1, m.h-fullTop-m.stripHeight()-footerH)
}

// graphWidth is how many columns the drawing may use: the frame insets it by
// one cell on each side, and every band is measured against this.
func (m *Model) graphWidth() int { return maxInt(1, m.w-2) }

// graphAlong is the ALONG-axis budget place() lays the layers out in — the
// drawing's width when the layers stack top-down, its height when they run
// left-to-right.
func (m *Model) graphAlong() int {
	if m.graphOrient == orientLeftRight {
		return m.graphCanvasH()
	}
	return m.graphWidth()
}

// graphFrame is one frame's negotiated geometry: the single measurement the
// renderer, the scroll clamp and the selection walk all read, so what is drawn
// and what a keystroke lands on can never disagree.
type graphFrame struct {
	orient     graphOrient
	titleLines int   // title lines inside a node box
	channels   []int // channel depth between rank r and r+1, on the ACROSS axis
	routes     [][]routedEdge

	// nodeW is a box's width. Left-right only: top-down spends the along axis,
	// so there each node carries its own Span.
	nodeW int

	// The rank window the drawing covers. Top-down always draws every rank, so
	// it is the whole range and hidden is 0.
	first, last, hidden int

	// overWidth is how far the drawing runs past the right edge. Only top-down
	// can be non-zero: its ALONG axis is the screen width and there is no
	// horizontal scroll, so a layer too wide is cut by renderGraph's pad. The
	// header reports it — this file's own rule is that a graph which quietly
	// omits a node is worse than one that admits it.
	overWidth int
}

func (f graphFrame) nodeH() int { return f.titleLines + graphNodeChrome }

// buildGraph lays out the ego graph for the current focus at the current
// radius and orientation.
//
// The key handlers do NOT call it — they read the cached m.graphLay, which
// renderGraph rewrites on every frame. That holds only because bubbletea calls
// View() after every Update, so a keystroke walks the geometry the previous
// frame drew. A handler that both invalidates the layout AND then navigates
// would be reading a stale one.
func (m *Model) buildGraph() *egoLayout {
	// graphHardCols in BOTH orientations, never a screen-derived cap: see its
	// doc comment. At the 240-column floor the width-derived formula this
	// replaced already evaluated to exactly graphHardCols.
	l := buildEgo(m.g, m.graphFocus, m.graphRadius, graphHardCols, m.taskHidden)
	l.place(m.graphOrient, m.graphAlong())
	return l
}

// graphMeasure routes every channel and then negotiates whichever axis place()
// did not spend. See rule 2 at the head of this file for why that differs by
// orientation.
func (m *Model) graphMeasure(l *egoLayout) graphFrame {
	f := graphFrame{orient: m.graphOrient, first: 0, last: len(l.Layers) - 1}
	for r := 0; r+1 < len(l.Layers); r++ {
		rt, d := routeChannel(l, l.rankEdges(r))
		f.routes = append(f.routes, rt)
		f.channels = append(f.channels, d)
	}

	if f.orient == orientLeftRight {
		f.titleLines = clamp(l.Span-graphNodeChrome, graphMinNodeLines, graphMaxNodeLines)
		f.first, f.last, f.hidden = graphRankWindow(l, f.channels, m.graphWidth(), m.graphSelRank(l))
		avail := m.graphWidth()
		if l.Empty() {
			// The notes sit BESIDE the lone box here, so they come out of the
			// same budget it does. Negotiated without them, a box clamped up to
			// graphNodeMaxW pushed the trailing note off the right edge at
			// every width under 240.
			avail -= lg.Width(graphNoUpstream) + lg.Width(graphNoStructure) + 4
		}
		f.nodeW = graphNodeMinWLR
		if n := f.last - f.first + 1; n > 0 {
			used := 0
			for r := f.first; r < f.last; r++ {
				used += f.channels[r]
			}
			f.nodeW = clamp((avail-used)/n, graphNodeMinWLR, graphNodeMaxW)
		}
		return f
	}

	sum := 0
	for _, d := range f.channels {
		sum += d
	}
	ranks := maxInt(1, len(l.Layers))
	per := (m.graphCanvasH() - sum) / ranks
	// A node box is border(2) + the id line + the meta line, so `per-chrome` is
	// the title budget. It is clamped, never trusted: on a tiny terminal the
	// graph scrolls instead of rendering boxes with no title at all.
	f.titleLines = clamp(per-graphNodeChrome, graphMinNodeLines, graphMaxNodeLines)
	f.overWidth = maxInt(0, l.Extent()-l.Along)
	return f
}

// graphSelRank is the rank the left-right window is grown from: the SELECTION's,
// not the focus's.
//
// The cursor has to be on a drawn box. Top-down's scroll follows the selection
// for exactly this reason, and the across axis has no scroll of its own, so
// walking past the drawn ranks would otherwise leave the reader moving an
// invisible cursor and re-rooting on a node no box ever showed.
func (m *Model) graphSelRank(l *egoLayout) int {
	if n := l.Node(m.graphSel); n != nil {
		return n.Rank
	}
	return l.FocusRank()
}

// graphRankWindow is which ranks the left-right frame can fit across the
// screen, grown outward from `at` — the selected node's rank, so the window
// follows the walk and the cursor is always on a drawn box.
//
// This is graphHardCols' mirror on the other axis. The along axis overflows
// into the scroll, but a rank that will not fit ACROSS cannot be scrolled to
// without cutting a CJK box mid-glyph, so it is dropped and counted, and the
// header says so next to the `z` that narrows the radius. On the measured board
// it never fires: the longest chain is 5 edges, so an ego graph is at most 6
// ranks, and 6 boxes at graphNodeMinWLR plus their channels fit the 240-column
// floor.
func graphRankWindow(l *egoLayout, channels []int, avail, at int) (first, last, hidden int) {
	n := len(l.Layers)
	if n == 0 {
		return 0, -1, 0
	}
	at = clamp(at, 0, n-1)
	first, last = at, at
	used := graphNodeMinWLR
	for first > 0 || last < n-1 {
		// Grow the shorter side first, upstream winning ties — the same
		// preference the cycle placement makes.
		up := first > 0 && (last == n-1 || (at-first) <= (last-at))
		grew := false
		for _, tryUp := range [2]bool{up, !up} {
			if (tryUp && first == 0) || (!tryUp && last == n-1) {
				continue
			}
			cost := graphNodeMinWLR
			if tryUp {
				cost += channels[first-1]
			} else {
				cost += channels[last]
			}
			if used+cost > avail {
				continue
			}
			used += cost
			if tryUp {
				first--
			} else {
				last++
			}
			grew = true
			break
		}
		if !grew {
			break
		}
	}
	return first, last, first + (n - 1 - last)
}

func (m *Model) renderGraph() string {
	l := m.buildGraph()
	m.graphLay = l
	m.clampGraphSel(l)
	f := m.graphMeasure(l)

	canvasH := m.graphCanvasH()
	// Exactly one of these runs. Either composes every node box in the graph,
	// so assigning one and overwriting it would render the whole frame twice on
	// every keystroke in the other orientation.
	var bands []string
	if f.orient == orientLeftRight {
		bands = m.graphBandsLeftRight(l, f, canvasH)
	} else {
		bands = m.graphBandsTopDown(l, f)
	}

	m.graphScroll = clamp(m.graphScroll, 0, maxInt(0, len(bands)-canvasH))
	m.graphScroll = m.scrollGraphToSel(l, f, len(bands), canvasH)

	shown := bands
	if len(shown) > canvasH {
		shown = shown[m.graphScroll:minInt(len(bands), m.graphScroll+canvasH)]
	}
	canvas := make([]string, 0, canvasH)
	for _, s := range shown {
		canvas = append(canvas, " "+pad(s, maxInt(1, m.w-2)))
	}
	for len(canvas) < canvasH {
		canvas = append(canvas, strings.Repeat(" ", maxInt(1, m.w)))
	}

	parts := []string{
		pad(m.graphTitleBar(l), m.w),
		pad(m.graphHeader(l, f, len(bands) > canvasH), m.w),
		strings.Join(canvas, "\n"),
	}
	if sh := m.stripHeight(); sh > 0 {
		parts = append(parts, m.graphStrip(l, sh))
	}
	parts = append(parts, pad(m.statusLine(), m.w))

	frame := m.fitFrame(strings.Join(parts, "\n"))
	// The graph is composed as a string, not through the compositor, so `?`
	// used to set fullHelp and change not one pixel here — the overlay was
	// simply never drawn, and the next Esc went on clearing an invisible flag.
	// Harmless while the graph had a footer of its own; a lie the moment this
	// view started advertising `? help` in its title bar. It is also the only
	// way to read the key surface from inside a full-screen mode.
	if m.fullHelp {
		frame = m.fitFrame(lg.NewCompositor(
			lg.NewLayer(frame).X(0).Y(0).Z(zChrome),
			m.helpLayer(),
		).Render())
	}
	return frame
}

// graphBandsTopDown stacks the bands as screen lines.
//
// bands is strictly one screen line per element. graphRankRows returns a
// multi-line block, so it is split here — appending it whole would make the
// scroll math count a 7-line rank as 1, and the per-line " "+pad in renderGraph
// would indent only the rank's top border (a 1-cell shear).
func (m *Model) graphBandsTopDown(l *egoLayout, f graphFrame) []string {
	var bands []string
	for r, row := range l.Layers {
		bands = append(bands, strings.Split(m.graphRankRows(row, f), "\n")...)
		if r < len(f.routes) {
			c := drawChannel(f.orient, l.Along, f.routes[r], f.channels[r])
			for _, line := range c.rows() {
				bands = append(bands, m.th.edge.Render(line))
			}
		}
	}
	// A focus with no structure at all: say so in words. A lone box floating in
	// an empty screen reads as a bug, not as an answer.
	if l.Empty() {
		bands = append([]string{m.th.dim.Render(graphNoUpstream), ""},
			append(bands, "", m.th.dim.Render(graphNoStructure))...)
	}
	return bands
}

// graphBand is one vertical slice of the left-right frame — a rank of boxes, a
// channel, or a note — already exactly w cells wide on every one of its rows.
type graphBand struct {
	w    int
	rows []string
}

// graphBandsLeftRight composes the bands side by side and flattens them to
// screen lines, the way mapBands does for the dep map's panels.
//
// The canvas is as tall as the tallest rank actually reaches, not as tall as
// the screen: a rank that does not fit runs past the bottom and the frame
// scrolls, rather than dropping nodes the other orientation would have shown.
func (m *Model) graphBandsLeftRight(l *egoLayout, f graphFrame, canvasH int) []string {
	h := maxInt(canvasH, l.Extent())

	var bands []graphBand
	// A focus with no structure at all: the notes go where the structure would
	// have been — nothing upstream on the left, nothing downstream on the
	// right. Top-down puts the same two lines above and below for that reason.
	if l.Empty() {
		bands = append(bands, m.graphNoteBand(l, graphNoUpstream, h))
	}
	for r := f.first; r <= f.last && r < len(l.Layers); r++ {
		bands = append(bands, graphBand{w: f.nodeW, rows: m.graphRankColumn(l.Layers[r], f, h)})
		if r < f.last && r < len(f.routes) {
			c := drawChannel(f.orient, h, f.routes[r], f.channels[r])
			rows := c.rows()
			for i := range rows {
				rows[i] = m.th.edge.Render(rows[i])
			}
			bands = append(bands, graphBand{w: f.channels[r], rows: rows})
		}
	}
	if l.Empty() {
		bands = append(bands, m.graphNoteBand(l, graphNoStructure, h))
	}

	total := 0
	for _, b := range bands {
		total += b.w
	}
	lead := strings.Repeat(" ", maxInt(0, (m.graphWidth()-total)/2))

	out := make([]string, h)
	var sb strings.Builder
	for y := 0; y < h; y++ {
		sb.Reset()
		sb.WriteString(lead)
		for _, b := range bands {
			if y < len(b.rows) {
				sb.WriteString(b.rows[y])
			}
		}
		out[y] = sb.String()
	}
	return out
}

// graphNoteBand is one line of prose set beside the drawing, on the row the
// focus box's own centre sits on so the eye reads them as one sentence.
func (m *Model) graphNoteBand(l *egoLayout, s string, h int) graphBand {
	w := lg.Width(s) + 2
	blank := strings.Repeat(" ", w)
	rows := make([]string, h)
	for i := range rows {
		rows[i] = blank
	}
	at := h / 2
	if n := l.FocusNode(); n != nil {
		at = n.Anchor()
	}
	if at >= 0 && at < h {
		rows[at] = " " + pad(m.th.dim.Render(s), w-2) + " "
	}
	return graphBand{w: w, rows: rows}
}

// graphRankColumn draws one rank as a column of exactly h lines, each exactly
// nodeW cells, so the bands to either side of it cannot drift.
func (m *Model) graphRankColumn(row []*egoNode, f graphFrame, h int) []string {
	blank := strings.Repeat(" ", f.nodeW)
	out := make([]string, h)
	for i := range out {
		out[i] = blank
	}
	for _, n := range row {
		var lines []string
		if n.Kind == egoDummy {
			// A pass-through is one rule carrying the line straight across the
			// rank it is only travelling through — one row, not a box's worth.
			lines = []string{m.th.edge.Render(strings.Repeat("─", f.nodeW))}
		} else {
			lines = strings.Split(m.renderGraphNode(n, f.nodeW, f.titleLines), "\n")
		}
		for j, ln := range lines {
			if y := n.Along + j; y >= 0 && y < h {
				out[y] = ln
			}
		}
	}
	return out
}

func (m *Model) graphTitleBar(l *egoLayout) string {
	th := m.th
	left := th.title.Render("furrow board") + th.crumb.Render("  ·  ") +
		m.fullTabs(viewGraph)
	// `? help` here too: the graph is a full-screen mode, so once its footer
	// went this row became the only pointer to the key surface from inside it.
	right := th.crumb.Render(fmt.Sprintf("%d nodes · %d edges  ·  ",
		len(l.Real()), len(l.Edges))) + th.accent.Render("⟨GRAPH⟩") +
		th.dim.Render("  ·  ? help")
	return joinEnds(left, right, m.w)
}

// graphHeader is the one line that says what you are looking at, which way is
// which, and how deep the walk went. The two direction words follow the
// orientation: they are half of the position-plus-arrowhead redundancy, and a
// header still saying "↑ blockers" over a left-right picture would be the
// reader's only cue, pointing the wrong way.
func (m *Model) graphHeader(l *egoLayout, f graphFrame, clipped bool) string {
	th := m.th
	focus := m.b.Task(l.Focus)
	name := l.Focus
	if focus != nil {
		name = l.Focus + " " + focus.Title
	}
	up, down := "↑ blockers", "↓ unblocks"
	if f.orient == orientLeftRight {
		up, down = "← blockers", "→ unblocks"
	}
	left := th.peekHdr.Render(up) + th.dim.Render(" / ") +
		th.peekHdr.Render(down) + th.dim.Render("  ·  rooted on ") +
		th.chipAlt.Render(ansi.Truncate(name, maxInt(10, m.w/2), "…"))

	bits := []string{fmt.Sprintf("radius %s", radiusLabel(l.Radius)),
		fmt.Sprintf("%d up / %d down", l.UpCount, l.DownCount)}
	if n := len(l.Skipped); n > 0 {
		bits = append(bits, th.warn.Render(fmt.Sprintf("%d cyclic edge(s) not drawn", n)))
	}
	if len(l.Overflow) > 0 {
		n := 0
		for _, v := range l.Overflow {
			n += v
		}
		bits = append(bits, th.warn.Render(fmt.Sprintf("+%d over the layer cap", n)))
	}
	if f.overWidth > 0 {
		bits = append(bits, th.warn.Render(
			fmt.Sprintf("%d cell(s) past the right edge", f.overWidth)))
	}
	if f.hidden > 0 {
		bits = append(bits, th.warn.Render(
			fmt.Sprintf("+%d layer(s) beyond the width — z narrows the radius", f.hidden)))
	}
	if clipped {
		bits = append(bits, th.dim.Render("^u/^d scroll"))
	}
	return joinEnds(left, th.dim.Render(strings.Join(bits, " · ")), m.w)
}

// graphRankRows composes one rank of node boxes into a rectangular band.
//
// Boxes are joined by SLICING each box's rendered lines and concatenating them
// with plain-space gaps, rather than by lipgloss.JoinHorizontal: every box is
// already exactly n.Span display cells wide and exactly the same height, so the
// concatenation is width-exact and the channel anchors below it line up to the
// cell.
func (m *Model) graphRankRows(row []*egoNode, f graphFrame) string {
	h := f.nodeH()
	lines := make([]string, h)

	type piece struct {
		x    int
		rows []string
	}
	var pieces []piece
	for _, n := range row {
		if n.Kind == egoDummy {
			col := make([]string, h)
			for i := range col {
				col[i] = m.th.edge.Render(pad(" │ ", n.Span))
			}
			pieces = append(pieces, piece{x: n.Along, rows: col})
			continue
		}
		box := m.renderGraphNode(n, n.Span, f.titleLines)
		pieces = append(pieces, piece{x: n.Along, rows: strings.Split(box, "\n")})
	}

	for i := 0; i < h; i++ {
		var b strings.Builder
		cur := 0
		for _, p := range pieces {
			if p.x > cur {
				b.WriteString(strings.Repeat(" ", p.x-cur))
				cur = p.x
			}
			var seg string
			if i < len(p.rows) {
				seg = p.rows[i]
			}
			b.WriteString(seg)
			cur += lg.Width(seg)
		}
		lines[i] = b.String()
	}
	return strings.Join(lines, "\n")
}

// renderGraphNode draws one node box: id line, title, metadata. This is the
// half of a graph view that is actually hard — a layered picture of 12 boxes is
// arithmetic, but a box that says something useful in 32-100 cells is design.
//
// At 240+ columns a top-down node gets 52-95 cells of inner width, so the
// median 82-cell title lands in one or two lines instead of the 22-37% stub a
// 140-column terminal could show. That is the entire argument for building this
// wide-first. Left-right spends the width on layers instead, so its boxes are
// narrower (32 inner at the 240 floor) but get all three title lines — 96 cells
// against the same median.
func (m *Model) renderGraphNode(n *egoNode, w, titleLines int) string {
	th := m.th
	inner := maxInt(4, w-4)
	t := m.b.Task(n.ID)

	var lines []string

	if t == nil {
		// A dep pointing at an id that is not on the board. Say exactly that.
		lines = append(lines, joinEnds(th.danger.Render(glyphUnknown+" "+n.ID), "", inner))
		for i := 0; i < titleLines; i++ {
			body := ""
			if i == 0 {
				body = th.dim.Render("not on this board")
			}
			lines = append(lines, pad(body, inner))
		}
		lines = append(lines, pad(th.dim.Render("unresolved dependency"), inner))
		return th.graphNodeUnknown.Width(w).Render(strings.Join(lines, "\n"))
	}

	glyph, styleFor := cardMarker(t, m.g)
	head := styleFor(th).Render(glyph) + " " + th.chipAlt.Render(t.ID)
	// Board.Lane is documented to return nil for a slug outside the board's
	// vocabulary, and laneDot takes a Lane BY VALUE and reads only .Name
	// precisely so an unknown lane degrades to the muted default. Dereferencing
	// here defeated both: such a task never reaches a column (recompute fills
	// m.cols for known lanes only) but IS drawn as a graph node whenever it
	// sits in the ego graph of whatever the user rooted on.
	ln := board.Lane{Name: t.Status}
	if l := m.b.Lane(t.Status); l != nil {
		ln = *l
	}
	right := th.laneDot(ln).Render(glyphLaneDot) + " " + th.muted.Render(t.Status)

	// The id and the lane always survive; everything else on this line is shed
	// when it will not fit.
	//
	// This is not decoration. joinEnds drops its LEFT end first, so a head line
	// assembled whole loses the ID before it loses the repo chip, and the id is
	// what ⏎ re-roots on and what the header and the strip cross-reference. At
	// the left-right frame's floor width that is the common case, not a corner.
	//
	// The list below is in KEEP order, so it reads as the reverse of the shed
	// order: the both-directions badge stays longest because nothing else on
	// any screen says it, the repo chip next because the strip says it too, and
	// the focus badge goes first because the double border AND the header
	// already say it. The loop BREAKS rather than skipping, so a small badge
	// cannot jump the queue past a big one that missed.
	both := "↕ both directions"
	if m.graphOrient == orientLeftRight {
		both = "↔ both directions"
	}
	keep := map[string]bool{}
	used := lg.Width(head) + lg.Width(right) + 1
	for _, e := range []struct {
		name string
		s    string
		sep  int
		want bool
	}{
		{"both", both, 1, n.Both},
		{"repo", t.ShortRepo(), 3, t.ShortRepo() != ""},
		{"focus", "◉ focus", 1, n.Focus},
	} {
		if !e.want {
			continue
		}
		if used+e.sep+lg.Width(e.s) > inner {
			break
		}
		used += e.sep + lg.Width(e.s)
		keep[e.name] = true
	}
	// Assembled in READING order, which is deliberately not the shed order.
	if keep["focus"] {
		head += " " + th.accent.Render("◉ focus")
	}
	if keep["both"] {
		head += " " + th.warn.Render(both)
	}
	if keep["repo"] {
		right += th.dim.Render(" · ") + th.chipAlt.Render(t.ShortRepo())
	}
	lines = append(lines, joinEnds(head, right, inner))

	body := wrapLines(t.Title, inner)
	if len(body) > titleLines {
		body = body[:titleLines]
		body[titleLines-1] = ansi.Truncate(body[titleLines-1], inner-1, "…")
	}
	titleStyle := th.base
	if m.g.IsDone(t.ID) {
		titleStyle = th.dim
	}
	for i := 0; i < titleLines; i++ {
		s := ""
		if i < len(body) {
			s = titleStyle.Render(body[i])
		}
		lines = append(lines, pad(s, inner))
	}

	var bits []string
	if t.Value > 0 || t.Effort > 0 {
		bits = append(bits, th.muted.Render(fmt.Sprintf("v%d e%d", t.Value, t.Effort)))
	}
	if nb := len(m.g.BlockedBy(t.ID)); nb > 0 {
		bits = append(bits, th.danger.Render(fmt.Sprintf("%s%d blocked", glyphBlocked, nb)))
	}
	if m.g.Actionable(t.ID) {
		bits = append(bits, th.ok.Render(glyphActionable+" actionable"))
	}
	if d, tot := t.CheckProgress(); tot > 0 {
		bits = append(bits, th.muted.Render(fmt.Sprintf("[%d/%d]", d, tot)))
	}
	for _, lb := range t.Labels {
		bits = append(bits, th.chipFor(lb).Render("●")+th.muted.Render(" "+lb))
	}
	meta := strings.Join(bits, th.dim.Render(" · "))
	tag := ""
	if n.Hidden {
		// The graph deliberately shows what the board filter hides — an edge
		// that disappears because of a query is a lie about the board — so the
		// node is MARKED rather than dropped.
		tag = th.warn.Render("filtered out")
	}
	lines = append(lines, joinEnds(meta, tag, inner))

	return m.graphNodeStyle(n, t).Width(w).Render(strings.Join(lines, "\n"))
}

func (m *Model) graphNodeStyle(n *egoNode, t *board.Task) lg.Style {
	th := m.th
	sel := n.Key == m.graphSel
	switch {
	case n.Focus && sel:
		return th.graphNodeFocusSel
	case n.Focus:
		return th.graphNodeFocus
	case sel:
		return th.graphNodeSel
	case t != nil && m.g.IsDone(t.ID):
		return th.graphNodeDone
	}
	return th.graphNode
}

// graphStrip resolves the graph's selection to a task and hands it to the
// shared detail strip.
func (m *Model) graphStrip(l *egoLayout, h int) string {
	n := l.Node(m.graphSel)
	if n == nil {
		n = l.FocusNode()
	}
	if n == nil {
		return m.taskStrip(nil, false, h)
	}
	return m.taskStrip(m.b.Task(n.ID), n.Hidden, h)
}

// ---- navigation -------------------------------------------------------------

// clampGraphSel keeps the selection on a node that still exists — a radius
// change or a re-root can remove the node the cursor was on.
func (m *Model) clampGraphSel(l *egoLayout) {
	if n := l.Node(m.graphSel); n != nil && n.Kind == egoReal {
		return
	}
	m.graphSel = l.Focus
}

// graphMove walks the selection. The key pair aligned with the LAYER axis
// crosses ranks keeping the nearest anchor (which is what makes the grid feel
// like a grid); the other pair walks within a rank. Which pair is which follows
// the orientation, so ↓ always means "further from the blockers". Dummies are
// routing artefacts and are skipped.
func (m *Model) graphMove(dx, dy int) {
	l := m.graphLay
	if l == nil {
		return
	}
	cur := l.Node(m.graphSel)
	if cur == nil {
		return
	}
	along, across := dx, dy
	if m.graphOrient == orientLeftRight {
		along, across = dy, dx
	}
	if along != 0 {
		row := l.Layers[cur.Rank]
		for i := cur.Slot + along; i >= 0 && i < len(row); i += along {
			if row[i].Kind == egoReal {
				m.graphSel = row[i].Key
				return
			}
		}
		return
	}
	if across == 0 {
		return
	}
	want := cur.Anchor()
	for r := cur.Rank + across; r >= 0 && r < len(l.Layers); r += across {
		best, bestD := "", 1<<30
		for _, n := range l.Layers[r] {
			if n.Kind != egoReal {
				continue
			}
			if d := abs(n.Anchor() - want); d < bestD {
				best, bestD = n.Key, d
			}
		}
		if best != "" {
			m.graphSel = best
			return
		}
	}
}

// scrollGraphToSel keeps the selected node's band on screen. The offsets are
// recomputed from the same frame the renderer used, so the scroll can never
// disagree with what is drawn.
func (m *Model) scrollGraphToSel(l *egoLayout, f graphFrame, total, canvasH int) int {
	if total <= canvasH {
		return 0
	}
	n := l.Node(m.graphSel)
	if n == nil {
		return clamp(m.graphScroll, 0, total-canvasH)
	}
	var top, bot int
	if f.orient == orientLeftRight {
		// The along axis IS the screen line axis here, so place() already
		// answered this.
		top, bot = n.Along, n.Along+n.Span
	} else {
		nodeH := f.nodeH()
		for r := 0; r < n.Rank; r++ {
			top += nodeH
			if r < len(f.channels) {
				top += f.channels[r]
			}
		}
		bot = top + nodeH
	}
	s := m.graphScroll
	if top < s {
		s = top
	}
	if bot > s+canvasH {
		s = bot - canvasH
	}
	return clamp(s, 0, total-canvasH)
}

// ---- keys -------------------------------------------------------------------

// openGraph roots the graph on the current selection and switches to it.
func (m *Model) openGraph() {
	t := m.curTask()
	if t == nil {
		m.note("nothing selected — the graph is rooted on a task")
		return
	}
	m.cancelDrag()
	m.graphFocus, m.graphSel = t.ID, t.ID
	m.graphScroll = 0
	m.graphStack = nil
	m.graphFrom = viewBoard
	m.view = viewGraph
	m.note("graph rooted on %s — ⏎ re-roots on the selected node · z cycles radius · o flips the axis · esc returns", t.ID)
}

// rerootGraph is the thing a static picture cannot do: walk the graph. The
// previous root is pushed so `<` retraces the walk.
func (m *Model) rerootGraph() {
	l := m.graphLay
	if l == nil {
		return
	}
	n := l.Node(m.graphSel)
	if n == nil || n.Kind != egoReal {
		return
	}
	if n.Key == m.graphFocus {
		m.note("%s is already the root — move the selection first", n.Key)
		return
	}
	if m.b.Task(n.ID) == nil {
		m.fail("%s is not on this board, so it has no structure to root on", n.ID)
		return
	}
	m.graphStack = append(m.graphStack, m.graphFocus)
	m.graphFocus, m.graphSel = n.Key, n.Key
	m.graphScroll = 0
	m.note("→ re-rooted on %s  ·  < retraces", n.Key)
}

func (m *Model) graphBack() {
	if len(m.graphStack) == 0 {
		m.note("graph walk is at its start")
		return
	}
	id := m.graphStack[len(m.graphStack)-1]
	m.graphStack = m.graphStack[:len(m.graphStack)-1]
	m.graphFocus, m.graphSel = id, id
	m.graphScroll = 0
	m.note("← back to %s (%d left)", id, len(m.graphStack))
}

func (m *Model) cycleGraphRadius() {
	for i, r := range graphRadii {
		if r == m.graphRadius {
			// The graph header prints the radius every frame; see model.go.
			m.graphRadius = graphRadii[(i+1)%len(graphRadii)]
			return
		}
	}
	m.graphRadius = graphRadii[0]
}

// cycleGraphOrient flips which screen axis the layers run along. No note, for
// the same reason the radius key has none: the header states the direction on
// every frame, in the two words that are half the redundancy contract.
//
// The scroll offset goes with it. It is screen lines in both orientations, but
// it counts lines of a frame laid out on the other axis, so carrying it over
// would land the window somewhere nothing chose.
func (m *Model) cycleGraphOrient() {
	if m.graphOrient == orientLeftRight {
		m.graphOrient = orientTopDown
	} else {
		m.graphOrient = orientLeftRight
	}
	m.graphScroll = 0
}

// closeGraph returns to the board, landing the board cursor on whatever node
// the graph walk ended on — the walk was navigation, so it should have moved
// you.
func (m *Model) closeGraph() {
	// Back to whichever view opened it. From the dep map the walk was a detour
	// INSIDE the overview, so landing on the board would throw away the thing
	// the reader was actually reading.
	if m.graphFrom == viewMap {
		m.graphFrom = viewBoard
		m.view = viewMap
		if l := m.graphLay; l != nil {
			if n := l.Node(m.graphSel); n != nil && n.Kind == egoReal {
				// Walking a graph and stopping on a node IS a choice, so the
				// map cursor that comes back from it is one the board may
				// follow — unlike the fallback row openMap had to invent.
				m.mapSel, m.mapMoved = n.ID, true
			}
		}
		m.mapScroll = 0
		m.note("dep map — the cursor followed the graph walk")
		return
	}
	m.view = viewBoard
	// graphLay is written only by renderGraph, so a driver that calls Update
	// without View — every headless harness in this package — reaches here with
	// it still nil, and Node dereferences its receiver. The two other readers
	// (graphMove, rerootGraph) already guard; this one did not.
	if l := m.graphLay; l != nil {
		if n := l.Node(m.graphSel); n != nil && n.Kind == egoReal {
			// Same rule as jumpToBlocker/jumpBack: pin only what the filter
			// would otherwise hide, so an unfiltered walk leaves no permanent
			// exemption behind.
			if !m.selectID(n.ID, false) {
				m.selectID(n.ID, true)
			}
		}
	}
	m.note("board view — the cursor followed the graph walk")
}

package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The dependency MAP view: every dependency cluster on the board at once.
// depmap.go decided the geometry; this file paints it.
//
// The rule that keeps it honest at 240-400 columns is the one the whole repo
// runs on: EVERY LINE IS COMPOSED TO EXACTLY ColW DISPLAY CELLS by pad(), so a
// Japanese title — two cells per glyph — cannot shear the column to its right.
// No rune grid appears anywhere in this file, because every row it draws
// contains text.
//
// Where the graph negotiates its height, this view negotiates its WIDTH: the
// column count falls out of the terminal, and the rows are then given whatever
// each column can afford. That is why the map is worth having at 400 columns
// and would not be at 120.

// mapBlockerBudget is the share of a row a "←t-a,t-b" tag may take. The tag is
// the map's whole disambiguation mechanism, so it outranks the title — but a
// node with six blockers must not leave the row with no title at all.
const mapBlockerBudget = 3

// mapCanvasH is how many rows the packed grid itself may use.
func (m *Model) mapCanvasH() int {
	return maxInt(1, m.h-fullTop-m.stripHeight()-footerH)
}

// buildMap groups the board and packs it for the current width.
//
// The key handlers do NOT call it — they read the cached m.mapLay, which
// renderMap rewrites on every frame. That is sound only because bubbletea
// calls View() after every Update, so the geometry a keystroke walks is the
// one the previous frame drew; it is not sound for a handler that both
// invalidates the layout and then navigates, which is why cycleMapScope
// changes nothing but the scope and lets the next frame rebuild.
func (m *Model) buildMap() *mapLayout {
	return packMap(m.mapScope, m.g.Clusters(m.mapScope), maxInt(1, m.w-2))
}

func (m *Model) renderMap() string {
	l := m.buildMap()
	m.mapLay = l
	m.clampMapSel(l)

	bands := m.mapBands(l)
	canvasH := m.mapCanvasH()
	m.mapScroll = clamp(m.mapScroll, 0, maxInt(0, len(bands)-canvasH))
	m.mapScroll = m.scrollMapToSel(l, len(bands), canvasH)

	shown := bands
	if len(shown) > canvasH {
		shown = shown[m.mapScroll:minInt(len(bands), m.mapScroll+canvasH)]
	}
	canvas := make([]string, 0, canvasH)
	for _, s := range shown {
		canvas = append(canvas, " "+pad(s, maxInt(1, m.w-2)))
	}
	for len(canvas) < canvasH {
		canvas = append(canvas, strings.Repeat(" ", maxInt(1, m.w)))
	}

	parts := []string{
		pad(m.mapTitleBar(l), m.w),
		pad(m.mapHeader(l, len(bands) > canvasH), m.w),
		strings.Join(canvas, "\n"),
	}
	if sh := m.stripHeight(); sh > 0 {
		parts = append(parts, m.taskStrip(m.b.Task(m.mapSel), m.taskHidden(m.mapSel), sh))
	}
	parts = append(parts, pad(m.statusLine(), m.w))

	frame := m.fitFrame(strings.Join(parts, "\n"))
	// Composed as a string rather than through the compositor, exactly like
	// the graph — so `?` must be layered on here explicitly or it would set a
	// flag and change not one pixel while the title bar advertises it.
	if m.fullHelp {
		frame = m.fitFrame(lg.NewCompositor(
			lg.NewLayer(frame).X(0).Y(0).Z(zChrome),
			m.helpLayer(),
		).Render())
	}
	return frame
}

// mapBands renders the packed grid to one string per screen line. Every column
// contributes exactly ColW cells at every row, so the join is width-exact and
// the columns cannot drift apart as the rows below them get longer.
func (m *Model) mapBands(l *mapLayout) []string {
	if l.Empty() {
		var msg string
		if l.Scope == board.ClusterOpen {
			msg = "— no open dependency clusters: nothing unfinished waits on anything else (z includes done) —"
		} else {
			msg = "— no dependency edges on this board at all —"
		}
		return []string{"", m.th.dim.Render(msg)}
	}

	blank := strings.Repeat(" ", l.ColW)
	cols := make([][]string, l.Cols)
	for c := range cols {
		cols[c] = make([]string, l.H)
		for y := range cols[c] {
			cols[c][y] = blank
		}
	}
	for _, p := range l.Panels {
		for j, line := range m.renderMapPanel(p, l.ColW) {
			if y := p.Y + j; y < l.H {
				cols[p.Col][y] = line
			}
		}
	}

	gap := strings.Repeat(" ", mapPanelGap)
	bands := make([]string, l.H)
	row := make([]string, l.Cols)
	for y := 0; y < l.H; y++ {
		for c := range cols {
			row[c] = cols[c][y]
		}
		bands[y] = strings.TrimRight(strings.Join(row, gap), " ")
	}
	return bands
}

// renderMapPanel draws one cluster: the rule that names it, one row per node,
// and the one line of arithmetic the overview exists to produce.
func (m *Model) renderMapPanel(p mapPanel, w int) []string {
	th := m.th
	c := p.Cluster
	out := make([]string, 0, p.H)

	label := fmt.Sprintf(" #%d  %d nodes · depth %d ", p.Num, len(c.Nodes), c.Depth())
	head := th.rule.Render("──") + th.peekHdr.Render(label)
	if n := w - lg.Width(head); n > 0 {
		head += th.rule.Render(strings.Repeat("─", n))
	}
	out = append(out, pad(head, w))

	for _, n := range c.Nodes {
		out = append(out, m.mapNodeRow(n, w))
	}

	// The facts only the overview can state, per cluster: where work can start,
	// how much is held up, and the single task holding up the most.
	//
	// Every number here is readable off the rows above it — unblocked/blocked/
	// done partition the panel and match the markers one for one. Deriving
	// them from pure topology instead put "7 unblocked" in green over seven
	// rows the same frame drew with `v`.
	bits := []string{
		th.ok.Render(fmt.Sprintf("%d unblocked", c.Roots())),
		th.danger.Render(fmt.Sprintf("%d blocked", c.Blocked())),
	}
	if d := c.Done(); d > 0 {
		bits = append(bits, th.dim.Render(fmt.Sprintf("%d done", d)))
	}
	if top := c.Top(); top.ID != "" {
		bits = append(bits, th.muted.Render(
			fmt.Sprintf("%s frees %d", top.ID, top.Blocking)))
	}
	out = append(out, pad(strings.Repeat(" ", mapSelGutter)+
		strings.Join(bits, th.dim.Render(" · ")), w))
	return out
}

// mapNodeRow is one node: selection gutter, depth indent, the board's own
// marker, the id, the title, and the blocker names right-aligned.
//
// The marker is cardMarker — the SAME vocabulary as the board card, the table
// and the graph node, deliberately not the prototype's own `*`/`x` pair. What
// the prototype meant by `*` ("startable now") is already carried structurally
// here and more precisely: a row at indent 0 with no `←` tag has nothing in
// scope blocking it, and the panel's stat line counts those rows. Inventing a
// fourth meaning for a glyph to say it twice would have cost more than it paid.
func (m *Model) mapNodeRow(n board.ClusterNode, w int) string {
	th := m.th
	t := m.b.Task(n.ID)
	sel := n.ID == m.mapSel

	gutter := strings.Repeat(" ", mapSelGutter)
	if sel {
		gutter = th.accent.Render("▌") + " "
	}
	if t == nil {
		// A cluster member is a board task by construction; if that ever stops
		// being true, say so rather than drawing a blank row.
		return pad(gutter+th.danger.Render(glyphUnknown+" "+n.ID)+
			th.dim.Render(" — not on this board"), w)
	}

	inner := maxInt(8, w-mapSelGutter)
	tag := m.blockerTag(n.Blockers, inner/mapBlockerBudget)

	glyph, styleFor := cardMarker(t, m.g)
	titleStyle := th.base
	switch {
	case m.g.IsDone(t.ID):
		titleStyle = th.dim
	case m.taskHidden(t.ID):
		// The map shows what the board filter hides — an edge that vanishes
		// because of a query is a lie about the board — so the row is MUTED,
		// never dropped. Same contract as the graph's "filtered out" tag,
		// spent on a style here because a row has no room for a second tag.
		titleStyle = th.dim
	}
	indent := strings.Repeat(" ", mapIndent(n.Depth))
	head := indent + styleFor(th).Render(glyph) + " " + th.chipAlt.Render(t.ID) + " "
	return pad(gutter+joinEnds(head+titleStyle.Render(t.Title), tag, inner), w)
}

// blockerTag names the node's blockers, dropping ids rather than cutting one
// in half: "←t-a,t-b +2" is true, "←t-a,t-b,t-c…" names a task that does not
// exist. It is the map's substitute for drawing a line, so it may never lie.
// depTag composes the tag and reports how many ids it managed to NAME. The
// colouring is deliberately not here: what a named id means differs per
// caller — a task blocker is live or done, an epic dep is open, closed or
// missing — and a shared colour rule would have to know both.
func depTag(ids []string, budget int) (string, int) {
	if len(ids) == 0 {
		return "", 0
	}
	shown := 0
	width := 1 // the leading arrow
	for _, id := range ids {
		w := lg.Width(id)
		if shown > 0 {
			w++ // the comma
		}
		if shown > 0 && width+w > budget {
			break
		}
		width += w
		shown++
	}
	tag := "←" + strings.Join(ids[:shown], ",")
	if rest := len(ids) - shown; rest > 0 {
		tag += fmt.Sprintf(" +%d", rest)
	}
	return tag, shown
}

func (m *Model) blockerTag(ids []string, budget int) string {
	tag, shown := depTag(ids, budget)
	if shown == 0 {
		return ""
	}
	// Colour by what the named ids actually DO right now. In scope=all the tag
	// also names deps that are already satisfied, and painting those as live
	// blockers would make a finished cluster look stuck.
	style := m.th.dim
	for _, id := range ids[:shown] {
		switch {
		case !m.g.Known(id):
			// An unresolved blocker is a defect, not a dependency, and it
			// outranks everything else in the tag.
			return m.th.danger.Render(tag)
		case !m.g.IsDone(id):
			style = m.th.warn
		}
	}
	return style.Render(tag)
}

// taskHidden reports whether the current board filter would hide the task.
// The three full-screen views with task rows (dep map, roadmap, swimlane) ask
// this one function: they make the same promise —
// dependency structure is drawn whole and filtered rows are MARKED, never
// dropped — and two copies of that predicate could disagree about which rows.
//
// lensOn, not qRaw: the slice term and the revisit lens filter these views
// exactly as they filter the board, which is taskVisible's contract mirrored
// for off-board ids. The lens narrows with an EMPTY query, so testing the
// query alone here muted nothing while the board hid 19 of 34 (measured).
func (m *Model) taskHidden(id string) bool {
	if id == "" || !m.lensOn() || m.pinned[id] || m.qMatched == nil {
		return false
	}
	t := m.b.Task(id)
	return t != nil && !m.qMatched[id]
}

func (m *Model) mapTitleBar(l *mapLayout) string {
	th := m.th
	left := th.title.Render("furrow board") + th.crumb.Render("  ·  ") +
		m.fullTabs(viewMap)
	right := th.crumb.Render(fmt.Sprintf("%d clusters · %d nodes  ·  ",
		len(l.Panels), len(l.Rows))) + th.accent.Render("⟨MAP⟩") +
		th.dim.Render("  ·  ? help")
	return joinEnds(left, right, m.w)
}

// mapHeader is the one line that says what is on screen and what the board as
// a whole looks like — the numbers that are only true when every cluster is
// in front of you.
func (m *Model) mapHeader(l *mapLayout, clipped bool) string {
	th := m.th
	left := th.peekHdr.Render("dependency clusters") + th.dim.Render("  ·  scope ") +
		th.chipAlt.Render(l.Scope.String())
	if l.Scope == board.ClusterOpen {
		left += th.dim.Render(" (done dropped)")
	}

	depth, unblocked, unresolved := 0, 0, 0
	var top board.ClusterNode
	for _, p := range l.Panels {
		if d := p.Cluster.Depth(); d > depth {
			depth = d
		}
		unblocked += p.Cluster.Roots()
		if t := p.Cluster.Top(); t.Blocking > top.Blocking {
			top = t
		}
		for _, n := range p.Cluster.Nodes {
			for _, b := range n.Blockers {
				if !m.g.Known(b) {
					unresolved++
				}
			}
		}
	}

	bits := []string{
		th.ok.Render(fmt.Sprintf("%d unblocked", unblocked)),
		fmt.Sprintf("longest chain %d", depth),
	}
	if top.ID != "" {
		bits = append(bits, fmt.Sprintf("%s frees %d", top.ID, top.Blocking))
	}
	if unresolved > 0 {
		bits = append(bits, th.warn.Render(fmt.Sprintf("%d dep(s) not on this board", unresolved)))
	}
	if bit := m.filterCountBit(m.mapHiddenCount(l)); bit != "" {
		bits = append(bits, bit)
	}
	if clipped {
		bits = append(bits, th.dim.Render("^u/^d page"))
	}
	return joinEnds(left, th.dim.Render(strings.Join(bits, " · ")), m.w)
}

func (m *Model) mapHiddenCount(l *mapLayout) int {
	n := 0
	for _, r := range l.Rows {
		if m.taskHidden(r.ID) {
			n++
		}
	}
	return n
}

// ---- navigation -------------------------------------------------------------

// clampMapSel keeps the cursor on a row that still exists — a scope change
// removes whole clusters.
func (m *Model) clampMapSel(l *mapLayout) {
	if l.Row(m.mapSel) != nil {
		return
	}
	if len(l.Rows) == 0 {
		m.mapSel = ""
		return
	}
	m.mapSel = l.Rows[0].ID
}

func (m *Model) mapMove(dx, dy int) {
	if m.mapLay == nil {
		return
	}
	if next := m.mapLay.step(m.mapSel, dx, dy); next != m.mapSel {
		m.mapSel, m.mapMoved = next, true
	}
}

func (m *Model) scrollMapToSel(l *mapLayout, total, canvasH int) int {
	return scrollToSel(m.mapScroll, total, canvasH, func() (int, int, bool) {
		r := l.Row(m.mapSel)
		if r == nil {
			return 0, 0, false
		}
		top := r.Y
		if p := l.Panels[r.Panel]; r.Y == p.Y+mapPanelHdr {
			top = p.Y
		}
		return top, r.Y, true
	})
}

// ---- keys -------------------------------------------------------------------

// openMap switches to the overview, landing on `seed` when that task is in a
// cluster at all — arriving from a task and losing it would make the map a
// place you go rather than a way to look at where you already are. The seed is
// passed in because the caller knows which cursor it means: the board's, or the
// graph's own selection.
func (m *Model) openMap(seed string) {
	m.cancelDrag()
	m.mapScroll = 0
	m.mapMoved = false
	m.mapSel = seed
	m.view = viewMap
	l := m.buildMap()
	m.mapLay = l
	if l.Row(m.mapSel) == nil {
		was := m.b.Task(m.mapSel)
		m.clampMapSel(l)
		if was != nil && m.mapSel != "" {
			// WHY the seed is not here decides the sentence. A task with dep
			// edges that the SCOPE dropped is not a task without dependencies,
			// and saying so was a flat falsehood for every done task with deps
			// and every open one whose deps are all done.
			if len(was.Deps) > 0 || len(m.g.Blocks(was.ID)) > 0 {
				m.note("dep map — %s has dependencies but none are open, so it is outside this scope · z includes done · ⏎ opens the graph · esc returns", was.ID)
			} else {
				m.note("dep map — %s has no dependencies, so the cursor went to the first cluster · ⏎ opens the graph · z scope · esc returns", was.ID)
			}
			return
		}
	}
	m.note("dep map — every cluster at once · ⏎ opens the graph on a row · z cycles scope · esc returns")
}

// closeMap returns to the board, landing the board cursor on the row the map
// walk ended on — the same contract as closing the graph.
func (m *Model) closeMap() {
	m.view = viewBoard
	m.carryCursorBack(m.mapMoved, m.mapSel)
	m.note("board view — the cursor followed the dep map")
}

// cycleMapScope flips between the live clusters and every cluster.
func (m *Model) cycleMapScope() {
	// The cursor may not survive the rebuild (clampMapSel), and a row the
	// clamp picked was not chosen by the user any more than the opening
	// fallback was.
	m.mapMoved = false
	if m.mapScope == board.ClusterOpen {
		m.mapScope = board.ClusterAll
	} else {
		m.mapScope = board.ClusterOpen
	}
	m.mapScroll = 0
	// No note: the header states the scope on every frame.
}

// graphFromMap roots the ego graph on the selected row. The map answers "what
// is tangled"; the graph answers "what exactly surrounds this one" — and the
// return path is recorded so esc lands back on the overview rather than
// dumping the reader onto the board.
func (m *Model) graphFromMap() {
	if m.mapSel == "" || m.b.Task(m.mapSel) == nil {
		m.note("no row selected — the graph is rooted on a task")
		return
	}
	m.graphFrom = viewMap
	m.graphFocus, m.graphSel = m.mapSel, m.mapSel
	m.graphScroll = 0
	m.graphStack = nil
	m.view = viewGraph
	m.note("graph rooted on %s — ⏎ re-roots · z cycles radius · o flips the axis · esc returns to the dep map", m.mapSel)
}

// onMapKey is the map's whole keyboard surface. Like the graph it is a reading
// and walking tool: nothing here writes to the board.
func (m *Model) onMapKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quitOrFlush()

	case key.Matches(msg, m.keys.Help):
		m.fullHelp = !m.fullHelp

	case key.Matches(msg, m.keys.Cancel):
		if m.fullHelp {
			m.fullHelp = false
			return nil
		}
		m.closeMap()

	case key.Matches(msg, m.keys.MapGraph):
		m.graphFromMap()

	case key.Matches(msg, m.keys.MapScope):
		m.cycleMapScope()

	case key.Matches(msg, m.keys.Map), key.Matches(msg, m.keys.View):
		m.closeMap()

	case key.Matches(msg, m.keys.PeekScroll):
		m.halfPage(msg, m.mapCanvasH(), func(dir int) bool {
			at := m.mapSel
			m.mapMove(0, dir)
			return m.mapSel != at
		}, "this column")

	case key.Matches(msg, m.keys.Up):
		m.mapMove(0, -1)
	case key.Matches(msg, m.keys.Down):
		m.mapMove(0, +1)
	case key.Matches(msg, m.keys.Left):
		m.mapMove(-1, 0)
	case key.Matches(msg, m.keys.Right):
		m.mapMove(+1, 0)
	}
	return nil
}

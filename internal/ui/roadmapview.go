package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The ROADMAP view: every open promise on one time axis. roadmap.go decided
// the geometry; this file paints it.
//
// The identity pane on the left is fixed and the TIMELINE is the negotiated
// half: whatever width the terminal has beyond the pane becomes cells, which
// is why this view is worth having at 240-400 columns. Every row is composed
// to exactly the frame's width by pad(), and the timeline half is REBUILT per
// window rather than sliced out of a styled string — windowing by ANSI
// surgery is how a CJK epic chip would shear the frame.

const (
	// roadPaneMaxW is the identity pane: gutter, marker, id, a recognisable
	// slice of the title, the date. The FULL title belongs to the strip — the
	// same answer every full-screen view gives — so this pane only has to make
	// a row findable.
	roadPaneMaxW = 48
	roadAxisH    = 2 // the two axis label rows over the timeline
	roadSepW     = 1 // the │ between the pane and the timeline
)

// roadPan is how many cells h/l move the window: one natural period per zoom
// — a week of days, a month of weeks, a quarter of months.
func roadPan(z roadZoom) int {
	switch z {
	case zoomWeek:
		return 4
	case zoomMonth:
		return 3
	}
	return 7
}

// roadPopulation is what the roadmap shows: every OPEN task that carries a
// due. Dateless tasks are absent — GH's roadmap draws nothing for an item
// with no date either — and so are done ones: a kept promise is not a
// promise, and the real board's done lane holds months of them. The filter
// deliberately does not shrink this population: like the graph and the map,
// the roadmap MUTES what the filter hides rather than dropping it
// (taskHidden), so a query can never make a deadline disappear.
func (m *Model) roadPopulation() []*board.Task {
	var out []*board.Task
	for _, t := range m.b.Tasks() {
		if !t.Due.IsZero() && !m.g.IsDone(t.ID) {
			out = append(out, t)
		}
	}
	return out
}

func (m *Model) buildRoad() *roadLayout {
	return packRoad(m.roadPopulation(), m.roadZoom, nowFn())
}

// roadPaneW is the identity pane's width this frame — the preferred constant,
// surrendering only when the terminal itself is narrower.
func (m *Model) roadPaneW() int {
	return clamp((m.w-2)/2, 1, roadPaneMaxW)
}

// roadTLW is the timeline window's width, in cells.
func (m *Model) roadTLW() int {
	return maxInt(1, m.w-2-m.roadPaneW()-roadSepW)
}

// roadRowsH is how many task rows fit between the axis and the strip.
func (m *Model) roadRowsH() int {
	return maxInt(1, m.h-fullTop-roadAxisH-m.stripHeight()-footerH)
}

func (m *Model) renderRoadmap() string {
	l := m.buildRoad()
	m.roadLay = l
	m.clampRoadSel(l)

	rowsH := m.roadRowsH()
	m.roadScroll = clamp(m.roadScroll, 0, maxInt(0, len(l.Rows)-rowsH))
	m.roadScroll = m.scrollRoadToSel(l, len(l.Rows), rowsH)
	tlW := m.roadTLW()
	if !m.roadAnchored && m.sized {
		// The opening window, placed on the first frame whose size is REAL —
		// not in startRoadmap (the -roadmap flag runs it inside New(), before
		// any size exists) and not on the interactive program's very first
		// frame either, which bubbletea draws before the WindowSizeMsg
		// arrives (found by review: the first two cuts each anchored against
		// the constructor's 240×60 on one of those paths). Today lands a
		// third of the way in — the promises just behind and just ahead of
		// it are the ones that need attention, and GH's roadmap opens the
		// same way. The seed may sit outside this window: its row is still
		// the selection (the strip shows it, its ◆ leaves an edge arrow),
		// and the first cursor move pans to it.
		m.roadAnchored = true
		m.roadXOff = 0
		if l.Cells > tlW {
			m.roadXOff = clamp(l.TodayX-tlW/3, 0, l.Cells-tlW)
		}
	}
	m.roadXOff = clamp(m.roadXOff, 0, maxInt(0, l.Cells-tlW))

	canvas := make([]string, 0, roadAxisH+rowsH)
	canvas = append(canvas, m.roadAxisRows(l, tlW)...)
	if l.Empty() {
		canvas = append(canvas, "", m.th.dim.Render(
			"— no open task carries a due: nothing here has a date · quick add's due: token or the edit overlay's due row puts one on —"))
	} else {
		for y := m.roadScroll; y < minInt(len(l.Rows), m.roadScroll+rowsH); y++ {
			canvas = append(canvas, m.roadRowLine(l, &l.Rows[y], tlW))
		}
	}
	full := make([]string, 0, roadAxisH+rowsH)
	for _, s := range canvas {
		full = append(full, " "+pad(s, maxInt(1, m.w-2)))
	}
	for len(full) < roadAxisH+rowsH {
		full = append(full, strings.Repeat(" ", maxInt(1, m.w)))
	}

	parts := []string{
		pad(m.roadTitleBar(l), m.w),
		pad(m.roadHeader(l, len(l.Rows) > rowsH, l.Cells > tlW), m.w),
		strings.Join(full, "\n"),
	}
	if sh := m.stripHeight(); sh > 0 {
		parts = append(parts, m.taskStrip(m.b.Task(m.roadSel), m.taskHidden(m.roadSel), sh))
	}
	parts = append(parts, pad(m.statusLine(), m.w))

	frame := m.fitFrame(strings.Join(parts, "\n"))
	// Composed as a string rather than through the compositor, exactly like
	// the map — so `?` must be layered on here explicitly or it would set a
	// flag and change not one pixel while the title bar advertises it.
	if m.fullHelp {
		frame = m.fitFrame(lg.NewCompositor(
			lg.NewLayer(frame).X(0).Y(0).Z(zChrome),
			m.helpLayer(),
		).Render())
	}
	return frame
}

// roadAxisRows composes the two axis label rows: a blank identity pane, the
// separator, and the windowed labels. The pane half stays blank on purpose —
// what the columns mean is the header line's job, and a per-pane caption
// would be the third partial copy of it.
func (m *Model) roadAxisRows(l *roadLayout, tlW int) []string {
	th := m.th
	// Labels stop at the AXIS end, not the window's: on a wide terminal the
	// window is usually longer than the axis, and a year of labelled cells
	// carrying no ◆ at all reads as content where there is none.
	coarse, fine := roadTicks(l.Zoom, l.start, m.roadXOff,
		minInt(tlW, maxInt(0, l.Cells-m.roadXOff)))
	pane := strings.Repeat(" ", m.roadPaneW())
	rows := make([]string, 0, roadAxisH)
	for _, ticks := range [][]roadTick{coarse, fine} {
		// Labels are ASCII (roadTicks' contract), so a byte overlay is a cell
		// overlay.
		row := []byte(strings.Repeat(" ", tlW))
		for _, tk := range ticks {
			copy(row[tk.X:], tk.Text)
		}
		rows = append(rows, pane+th.rule.Render("│")+th.muted.Render(string(row)))
	}
	return rows
}

// roadRowLine is one dated task: identity pane, separator, and the windowed
// timeline with its ◆.
func (m *Model) roadRowLine(l *roadLayout, r *roadRow, tlW int) string {
	th := m.th
	t := m.b.Task(r.ID)
	gutter := strings.Repeat(" ", mapSelGutter)
	if r.ID == m.roadSel {
		gutter = th.accent.Render("▌") + " "
	}
	paneW := m.roadPaneW()
	if t == nil {
		// A row is a board task by construction; if that ever stops being
		// true, say so rather than drawing a blank row.
		return pad(gutter+th.danger.Render(glyphUnknown+" "+r.ID)+
			th.dim.Render(" — not on this board"), paneW+roadSepW+tlW)
	}

	// The pane: marker, id, title — and the date the whole view is about,
	// right-aligned so it survives the CJK title's ellipsis (numbers outlive
	// prose, the house pattern).
	glyph, styleFor := cardMarker(t, m.g)
	dateStyle := th.muted
	if isOverdue(t) {
		dateStyle = th.danger
	}
	titleStyle := th.base
	if m.taskHidden(t.ID) {
		// Same contract as the map: the roadmap shows what the filter hides —
		// a deadline that vanishes because of a query is a lie about the
		// board — so the row is MUTED, never dropped.
		titleStyle = th.dim
	}
	inner := maxInt(1, paneW-mapSelGutter)
	head := styleFor(th).Render(glyph) + " " + th.chipAlt.Render(t.ID) + " "
	pane := gutter + joinEnds(head+titleStyle.Render(t.Title),
		dateStyle.Render(t.Due.Local().Format("01-02")), inner)

	return pane + th.rule.Render("│") + m.roadCells(l, t, r, tlW)
}

// roadCells is the row's timeline window: today's ┊, the ◆ at the due, the
// epic chip riding to its right — and, when the ◆ is panned out of the
// window, an edge arrow instead of a blank, because a dated row whose date is
// off screen must not read as dateless.
func (m *Model) roadCells(l *roadLayout, t *board.Task, r *roadRow, tlW int) string {
	th := m.th
	x := r.X - m.roadXOff
	tx := l.TodayX - m.roadXOff

	// seg is [from, to) cells of empty timeline, with today's gridline drawn
	// where it crosses.
	seg := func(from, to int) string {
		if to <= from {
			return ""
		}
		if tx >= from && tx < to {
			return strings.Repeat(" ", tx-from) + th.chipAlt.Render(glyphToday) +
				strings.Repeat(" ", to-tx-1)
		}
		return strings.Repeat(" ", to-from)
	}

	// glyphDropL/R read as DIRECTION here, not insertion — the same licence
	// the graph arrowheads cite for reusing sortDesc's triangle: a pointing
	// triangle reads as pointing on every surface.
	edge := th.dim
	if isOverdue(t) {
		edge = th.danger
	}
	switch {
	case x < 0:
		return edge.Render(glyphDropR) + seg(1, tlW)
	case x >= tlW:
		return seg(0, tlW-1) + edge.Render(glyphDropL)
	}

	dia := th.base
	switch {
	case isOverdue(t):
		dia = th.danger
	case r.X == l.TodayX:
		// Due TODAY: not yet a broken promise, but the one kind the reader
		// must not scan past.
		dia = th.warn
	}
	out := seg(0, x) + dia.Render(glyphDue)
	rest := tlW - x - 1
	if rest <= 0 {
		return out
	}

	// The epic chip rides the empty cells to the ◆'s right — the row-level
	// substitute for GH's vertical markers, which need dates no furrow epic
	// has. Resolved to its title like every other surface; the raw e- id in a
	// frame is a leak two views already assert against.
	//
	// The chip YIELDS to today's gridline: on every overdue row that belongs
	// to a box the chip's natural span crosses today, and letting it win put
	// a hole in the ┊ on exactly the rows the view exists to surface (found
	// by review — all three shipped headless frames had it).
	chip := ""
	budget := rest - 3
	if tx > x {
		budget = minInt(budget, tx-x-4)
	}
	if t.Epic != "" && budget > 0 {
		label := t.Epic
		if e := m.b.Epic(t.Epic); e != nil {
			label = e.Title
		}
		st := th.muted
		if m.taskHidden(t.ID) {
			st = th.dim
		}
		chip = " " + st.Render(glyphEpic+" "+ansi.Truncate(label, budget, "…"))
	}
	return out + chip + seg(x+1+lg.Width(chip), tlW)
}

func (m *Model) roadTitleBar(l *roadLayout) string {
	th := m.th
	right := th.crumb.Render(fmt.Sprintf("%d dated tasks  ·  ", len(l.Rows))) +
		th.accent.Render("⟨ROADMAP⟩") + th.dim.Render("  ·  ? help")
	// The saved-view tabs render here too: the roadmap is the one full-screen
	// view a saved view can BE, so landing on a roadmap tab must not hide the
	// tab strip that got you there (viewtabs.go). Right first, then the strip
	// budgeted to what remains — chromeLayers' rule.
	prefix := th.title.Render("furrow board") + th.crumb.Render("  ·  ") + m.fullTabs(viewRoadmap)
	left := prefix + m.viewTabStrip(m.w-lg.Width(prefix)-lg.Width(right)-1)
	return joinEnds(left, right, m.w)
}

// roadHeader is the one line that says what the axis is and what is at stake
// on it — the overdue count is only true when every promise is in front of
// you, which is exactly this view.
func (m *Model) roadHeader(l *roadLayout, clippedY, clippedX bool) string {
	th := m.th
	left := th.peekHdr.Render("due timeline") + th.dim.Render("  ·  zoom ") +
		th.chipAlt.Render(l.Zoom.String()) +
		th.dim.Render(" (1 cell = 1 "+l.Zoom.String()+")") +
		th.dim.Render("  ·  dateless and done dropped")

	over := 0
	for i := range l.Rows {
		if t := m.b.Task(l.Rows[i].ID); t != nil && isOverdue(t) {
			over++
		}
	}
	var bits []string
	if over > 0 {
		bits = append(bits, th.danger.Render(fmt.Sprintf("%d overdue", over)))
	}
	// The hidden count is an aggregate claim ABOUT THE FILTER, so it must not
	// be made from a verdict the store refused — mapHeader's rule, verbatim.
	if m.qErr != "" {
		bits = append(bits, th.warn.Render("filter refused — this count is from the last good verdict"))
	} else if hidden := m.roadHiddenCount(l); hidden > 0 {
		bits = append(bits, th.warn.Render(fmt.Sprintf("%d hidden by the filter", hidden)))
	}
	if clippedX {
		bits = append(bits, th.dim.Render("h/l pan"))
	}
	if clippedY {
		bits = append(bits, th.dim.Render("^u/^d page"))
	}
	return joinEnds(left, th.dim.Render(strings.Join(bits, " · ")), m.w)
}

func (m *Model) roadHiddenCount(l *roadLayout) int {
	n := 0
	for i := range l.Rows {
		if m.taskHidden(l.Rows[i].ID) {
			n++
		}
	}
	return n
}

// ---- navigation -------------------------------------------------------------

// clampRoadSel keeps the cursor on a row that still exists — a reload can
// close or re-date the task it was on.
func (m *Model) clampRoadSel(l *roadLayout) {
	if l.Row(m.roadSel) != nil {
		return
	}
	if len(l.Rows) == 0 {
		m.roadSel = ""
		return
	}
	m.roadSel = l.Rows[0].ID
}

func (m *Model) roadMove(dy int) {
	if m.roadLay == nil {
		return
	}
	if next := m.roadLay.step(m.roadSel, dy); next != m.roadSel {
		m.roadSel, m.roadMoved = next, true
		m.roadEnsureX()
	}
}

// roadJump is g/G: the earliest and the latest promise.
func (m *Model) roadJump(last bool) {
	l := m.roadLay
	if l == nil || len(l.Rows) == 0 {
		return
	}
	id := l.Rows[0].ID
	if last {
		id = l.Rows[len(l.Rows)-1].ID
	}
	if id != m.roadSel {
		m.roadSel, m.roadMoved = id, true
		m.roadEnsureX()
	}
}

// roadEnsureX pans the window the minimum that puts the selected ◆ back on
// screen, one pad cell in. Called from the paths that MOVE THE SELECTION,
// never from render: a window panned away from the selection is a legitimate
// state, exactly as a column scrolled away from its cursor is
// (ensureVisible's rule).
func (m *Model) roadEnsureX() {
	l := m.roadLay
	// An unanchored window does not exist yet — render owns the placement,
	// and nudging the offset before it would pan nothing.
	if l == nil || !m.roadAnchored {
		return
	}
	r := l.Row(m.roadSel)
	if r == nil {
		return
	}
	tlW := m.roadTLW()
	// The pad cell exists only when the window can spare it: at tlW==1 a ±1
	// pad IS the whole window, and both branches parked the ◆ one cell
	// outside it (found by review, in two rounds — the first fix stopped the
	// branches chaining and still overshot).
	pad := minInt(1, tlW-1)
	// else-if, deliberately: the second test must read the offset the FIRST
	// one was judged against.
	if r.X < m.roadXOff {
		m.roadXOff = maxInt(0, r.X-pad)
	} else if r.X >= m.roadXOff+tlW {
		m.roadXOff = minInt(r.X-tlW+1+pad, maxInt(0, l.Cells-tlW))
	}
}

// scrollRoadToSel keeps the selected row on screen, against the same row
// indexes the renderer draws, so the scroll can never disagree with the
// drawing.
func (m *Model) scrollRoadToSel(l *roadLayout, total, rowsH int) int {
	if total <= rowsH {
		return 0
	}
	r := l.Row(m.roadSel)
	if r == nil {
		return clamp(m.roadScroll, 0, total-rowsH)
	}
	s := m.roadScroll
	if r.Y < s {
		s = r.Y
	}
	if r.Y >= s+rowsH {
		s = r.Y - rowsH + 1
	}
	return clamp(s, 0, total-rowsH)
}

func (m *Model) roadPanBy(d int) {
	l := m.roadLay
	if l == nil || !m.roadAnchored {
		// Panning an unplaced window would move nothing (roadEnsureX's rule).
		return
	}
	m.roadXOff = clamp(m.roadXOff+d*roadPan(m.roadZoom), 0, maxInt(0, l.Cells-m.roadTLW()))
}

// ---- keys -------------------------------------------------------------------

// startRoadmap is openRoadmap minus the status line, because the -roadmap
// flag opens the view from inside New() — where a note would overwrite the
// read-only warning that is set exactly once per session and never restored
// (dump.go's own switch documents that trap; -table dodges it by being a
// bare view assignment, and this split is how -roadmap dodges it — found by
// review). It returns the fallback sentence the interactive path owes, ""
// when the seed landed.
//
// The seed is the caller's cursor when that task is on the axis at all —
// arriving from a dated task and losing it would make the roadmap a place
// you go rather than a way to look at when you already are. The WINDOW is
// deliberately not placed here: roadXOff's sentinel defers it to the first
// render, the one place the real terminal width is known (renderRoadmap).
func (m *Model) startRoadmap() string {
	seed := ""
	if t := m.curTask(); t != nil {
		seed = t.ID
	}
	return m.startRoadmapFrom(seed)
}

// startRoadmapFrom is startRoadmap with the seed made explicit. A tab
// switch must carry roadSel directly: the roadmap MUTES what the filter
// hides rather than dropping it, so its cursor is routinely on a task the
// board cols do not contain — a round trip through the board cursor
// (selectID, then curTask inside this function) dropped exactly those rows
// and snapped the walk back (found by review, on the second pass).
func (m *Model) startRoadmapFrom(seed string) string {
	m.cancelDrag()
	m.roadScroll, m.roadXOff, m.roadMoved, m.roadAnchored = 0, 0, false, false
	m.roadSel = seed
	m.view = viewRoadmap
	l := m.buildRoad()
	m.roadLay = l
	if l.Row(m.roadSel) == nil {
		was := m.b.Task(m.roadSel)
		m.clampRoadSel(l)
		if was != nil && m.roadSel != "" {
			// WHY the seed is not here decides the sentence — a done task with
			// a due is not a task without one, and the map's opening fallback
			// records what saying the wrong reason costs.
			if was.Due.IsZero() {
				return fmt.Sprintf("roadmap — %s carries no due, so the cursor went to the first promise · z zoom · esc returns", was.ID)
			}
			return fmt.Sprintf("roadmap — %s is done, so the cursor went to the first open promise · z zoom · esc returns", was.ID)
		}
	}
	return ""
}

func (m *Model) openRoadmap() {
	if s := m.startRoadmap(); s != "" {
		m.note("%s", s)
		return
	}
	m.note("roadmap — every open task with a due, on one time axis · z zoom · h/l pan · esc returns")
}

// closeRoadmap returns to the board, landing the board cursor on the row the
// walk ended on — the same contract as closing the map, fallback rows and
// all.
func (m *Model) closeRoadmap() {
	m.view = viewBoard
	if m.roadMoved && m.roadSel != "" {
		// Pin only what the filter would otherwise hide, so a walk over an
		// unfiltered board leaves no permanent exemption behind.
		if !m.selectID(m.roadSel, false) {
			m.selectID(m.roadSel, true)
		}
	}
	m.note("board view — the cursor followed the roadmap")
}

// cycleRoadZoom flips day → week → month → day. Unlike a scope toggle it
// rebuilds the layout EAGERLY: the axis changes units under the window, so
// the old offset means nothing and the new one must be derived from the new
// geometry — re-anchored on the selection when there is one, today otherwise.
func (m *Model) cycleRoadZoom() {
	switch m.roadZoom {
	case zoomDay:
		m.roadZoom = zoomWeek
	case zoomWeek:
		m.roadZoom = zoomMonth
	default:
		m.roadZoom = zoomDay
	}
	l := m.buildRoad()
	m.roadLay = l
	tlW := m.roadTLW()
	anchor := l.TodayX
	if r := l.Row(m.roadSel); r != nil {
		anchor = r.X
	}
	// A deliberate absolute placement counts as the anchor — pressing z is
	// the user acting on a window, so render must not re-place it. Gated on
	// sized like every other anchoring path: a z racing ahead of the first
	// WindowSizeMsg would otherwise anchor against the constructor's default
	// width, and the offset below stays a provisional value the sized render
	// overwrites (found by review — latent, but the invariant is "nothing
	// anchors against an unreal size", without exceptions).
	if m.sized {
		m.roadAnchored = true
	}
	m.roadXOff = 0
	if l.Cells > tlW {
		m.roadXOff = clamp(anchor-tlW/2, 0, l.Cells-tlW)
	}
	// No note: the header names the zoom on every frame.
}

// onRoadKey is the roadmap's whole keyboard surface. Like the map it is a
// reading tool: nothing here writes to the board — the task defers the
// due-editing drag until the read-only form has proven its worth.
func (m *Model) onRoadKey(msg tea.KeyPressMsg) tea.Cmd {
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
		m.closeRoadmap()

	case key.Matches(msg, m.keys.RoadZoom):
		m.cycleRoadZoom()

	// The saved-view keys work here because the title row shows the tabs
	// here — a strip rendered in the one view that can BE a tab, with its
	// keys dead in exactly that view, would be the advertised-but-unbound
	// failure t-84r1 wrote the rule about.
	case key.Matches(msg, m.keys.ViewTab):
		return m.switchView(viewTabDigit(msg))
	case key.Matches(msg, m.keys.ViewSave):
		m.saveView()

	case key.Matches(msg, m.keys.Roadmap), key.Matches(msg, m.keys.View):
		m.closeRoadmap()

	case key.Matches(msg, m.keys.Top):
		m.roadJump(false)
	case key.Matches(msg, m.keys.Bottom):
		m.roadJump(true)

	case key.Matches(msg, m.keys.PeekScroll):
		// Half a page of ROWS, not of scroll offset — the window is pinned to
		// the cursor by scrollRoadToSel on every frame, the same conflict the
		// map's ^u/^d comment resolves the same way.
		dir := 1
		if msg.String() != "ctrl+d" {
			dir = -1
		}
		before := m.roadSel
		for i := maxInt(1, m.roadRowsH()/2); i > 0; i-- {
			at := m.roadSel
			m.roadMove(dir)
			if m.roadSel == at {
				break
			}
		}
		if m.roadSel == before {
			m.note("already at the %s of the timeline", endName(dir))
		}

	case key.Matches(msg, m.keys.Up):
		m.roadMove(-1)
	case key.Matches(msg, m.keys.Down):
		m.roadMove(+1)
	case key.Matches(msg, m.keys.Left):
		m.roadPanBy(-1)
	case key.Matches(msg, m.keys.Right):
		m.roadPanBy(+1)
	}
	return nil
}

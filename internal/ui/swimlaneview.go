package ui

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The SWIMLANE view: the board's lanes across, a grouping axis down.
// swimlane.go decided the geometry; this file paints it.
//
// Every line is composed the same way — a RAIL of exactly RailW cells, then
// one segment of exactly ColW per drawn lane — so the band header, the task
// rows, the lane bar and the blank filler all measure identically and the
// columns cannot drift apart as CJK titles get longer below them. That is one
// funnel (swimLine) rather than four call sites, because the drift accumulates
// one cell at a time and only shows up at a width nobody dumped.
//
// The rail is what buys a band label real width: with the counts anchored at
// each lane column's right edge, a label sharing the lane row would have to
// stop before lane 0's count. On task rows the rail is the selection gutter
// and the band's continuation rule, so it is never dead space.

const (
	swimSelGutter = 2 // the selected row's left bar, for the reason mapSelGutter gives
	swimBarH      = 2 // the lane bar and its rule: sticky, never scrolled
)

// swimFoldOpen / swimFoldShut are the disclosure markers. ASCII because every
// triangle in this repo is already spoken for — ▸ actionable, ▶ the box a repo
// works out of, ◆ pinned, ▼/▶ the graph's arrowheads — and reusing one here
// would put a second meaning one row above cards carrying the first.
const (
	swimFoldOpen = "[-]"
	swimFoldShut = "[+]"
)

// swimSentinel is what a band with no value is called, per axis. The slice
// panel has no such row because it is a picker; the swimlane claims to be a
// partition of the population, and on the real board the unfiled pile is 119
// of 296 open tasks — two fifths of the board would otherwise be missing from
// a view whose whole promise is that everything is somewhere.
//
// "draft" for the repo axis, not the box overview's "(no repo)": glossary.md
// gives draft as furrow's word for a repo-less TASK, and boxNoRepoLabel is the
// label for a repo-less BOX.
func swimSentinel(axis sliceField) string {
	switch axis {
	case sliceRepo:
		return "(draft — no repo)"
	case sliceLabel:
		return "(no label)"
	}
	return "(unfiled — no box)"
}

// swimNoTerm is the -q term that would select the sentinel band. furrow can
// express it; sliceTerm cannot (it emits `field:value`, and the value here is
// absence), so ⏎ names the query to type instead of issuing one that means
// something else. The slice panel refuses off-axis keys the same way — with
// the reason, never silently.
func swimNoTerm(axis sliceField) string {
	switch axis {
	case sliceRepo:
		return "is:draft"
	case sliceLabel:
		return "no:label"
	}
	return "no:epic"
}

// swimVocab is the band ORDER for the current axis, taken from the same source
// that axis already uses everywhere else: repo and label from the sorted vocab
// the slice panel and the edit overlay share, epic from furrow's own order
// (active first, then pinned, then open by id, then closed) — which is what
// `furrow ls --tree` groups by, and this view is that command's second
// dimension. The sentinel is appended AFTER, never sorted in: its key is "".
func (m *Model) swimVocab() []swimValue {
	var out []swimValue
	switch m.swimAxis {
	case sliceRepo:
		for _, r := range m.repoVocab() {
			short := r
			if i := strings.LastIndex(r, "/"); i >= 0 {
				short = r[i+1:]
			}
			out = append(out, swimValue{Key: r, Label: short})
		}
	case sliceLabel:
		for _, l := range m.labelVocab() {
			out = append(out, swimValue{Key: l, Label: l})
		}
	case sliceEpic:
		// EpicsAll, not Epics: a task whose box has been CLOSED still has to
		// land somewhere, and the open-only vocabulary would drop it into the
		// unfiled sentinel — which would be a lie about where it is filed.
		known := map[string]bool{}
		for _, e := range m.b.EpicsAll() {
			known[e.ID] = true
			// Closed leads, the box overview's precedence: a closed box drawn
			// as `◆ pinned` says the wrong thing about the only state that
			// stops it from being work. (furrow clears active on close but
			// leaves pinned, so the pair is reachable.)
			marker := ""
			switch {
			case !e.Closed.IsZero():
				marker = glyphDone
			case e.Active:
				marker = glyphEpicActive
			case e.Pinned:
				marker = glyphEpicPinned
			}
			out = append(out, swimValue{Key: e.ID, Label: e.Title, Marker: marker})
		}
		// An epic id no box resolves gets a band under its RAW id — the
		// fallback the card, the table and the peek already use. The
		// vocabulary has to be closed over the population or packSwim has
		// nowhere to put that task, and it would leave every band, the title
		// bar's count and the lane bar without a word anywhere on the frame.
		// A stray id outside the current scope costs nothing: packSwim drops
		// bands with no members.
		stray := map[string]bool{}
		for _, t := range m.b.Tasks() {
			if t.Epic != "" && !known[t.Epic] {
				stray[t.Epic] = true
			}
		}
		for _, id := range slices.Sorted(maps.Keys(stray)) {
			out = append(out, swimValue{Key: id, Label: id})
		}
	}
	return append(out, swimValue{Key: "", Label: swimSentinel(m.swimAxis)})
}

// swimPopulation is what the view groups: every task on the board, minus the
// done ones unless the scope says otherwise. Built from the board rather than
// m.cols because the filter MUTES here and does not shrink — the same contract
// the dep map and the roadmap keep, and for the same reason: a band count that
// moves with a query is no longer a fact about the board.
func (m *Model) swimPopulation() map[string][]*board.Task {
	out := map[string][]*board.Task{}
	for _, l := range m.b.Lanes() {
		ts := m.b.LaneTasks(l.Name)
		if m.swimAll {
			out[l.Name] = ts
			continue
		}
		keep := make([]*board.Task, 0, len(ts))
		for _, t := range ts {
			if !m.g.IsDone(t.ID) {
				keep = append(keep, t)
			}
		}
		out[l.Name] = keep
	}
	return out
}

func (m *Model) buildSwim() *swimLayout {
	return packSwim(swimSpec{
		Axis: m.swimAxis, All: m.swimAll, Vocab: m.swimVocab(),
		Lanes: m.b.Lanes(), Cols: m.swimPopulation(), Open: m.swimOpen, W: m.w,
	})
}

// swimCanvasH is how many band lines fit between the lane bar and the strip.
func (m *Model) swimCanvasH() int {
	return maxInt(1, m.h-fullTop-swimBarH-m.stripHeight()-footerH)
}

func (m *Model) renderSwim() string {
	l := m.buildSwim()
	m.swimLay = l
	m.clampSwimSel(l)

	canvasH := m.swimCanvasH()
	total := l.Height()
	m.swimScroll = clamp(m.swimScroll, 0, maxInt(0, total-canvasH))
	m.swimScroll = m.scrollSwimToSel(l, total, canvasH)

	inner := maxInt(1, m.w-2)
	canvas := make([]string, 0, swimBarH+canvasH)
	canvas = append(canvas, m.swimBarRows(l)...)
	if l.Empty() {
		canvas = append(canvas, "", m.th.dim.Render(m.swimEmptyLine()))
	} else {
		// Only the visible window is rendered. The dep map and the box
		// overview compose every line and slice afterwards; this view does not,
		// and the divergence is deliberate — unfolding the real board's
		// biggest bands puts ~800 lines × 8 segments through ansi.Truncate,
		// which is the class CLAUDE.md's 36ms/frame measurement records. The
		// geometry is pure and index-addressable, so the window is an exact
		// range rather than a render-then-cut.
		for i := m.swimScroll; i < minInt(total, m.swimScroll+canvasH); i++ {
			canvas = append(canvas, m.swimLineText(l, l.Lines[i]))
		}
	}
	full := make([]string, 0, swimBarH+canvasH)
	for _, s := range canvas {
		full = append(full, " "+pad(s, inner))
	}
	for len(full) < swimBarH+canvasH {
		full = append(full, strings.Repeat(" ", maxInt(1, m.w)))
	}

	parts := []string{
		pad(m.swimTitleBar(l), m.w),
		pad(m.swimHeader(l, total > canvasH), m.w),
		strings.Join(full, "\n"),
	}
	if sh := m.stripHeight(); sh > 0 {
		id := l.IDOf(m.swimSel)
		parts = append(parts, m.taskStrip(m.b.Task(id), m.taskHidden(id), sh))
	}
	parts = append(parts, pad(m.statusLine(), m.w))

	frame := m.fitFrame(strings.Join(parts, "\n"))
	// Composed as a string rather than through the compositor, exactly like the
	// map and the roadmap — so `?` must be layered on here explicitly or it
	// would set a flag and change not one pixel while the title bar advertises
	// it.
	if m.fullHelp {
		frame = m.fitFrame(lg.NewCompositor(
			lg.NewLayer(frame).X(0).Y(0).Z(zChrome),
			m.helpLayer(),
		).Render())
	}
	return frame
}

func (m *Model) swimEmptyLine() string {
	// The all-done sentence is only true of a board that HAS tasks: said over
	// an empty store it asserted a fact about a population that does not exist.
	if !m.swimAll && len(m.b.Tasks()) > 0 {
		return "— every task on this board is done: nothing open to group (z includes done) —"
	}
	return "— this board holds no tasks at all —"
}

func (m *Model) swimTitleBar(l *swimLayout) string {
	th := m.th
	left := th.title.Render("furrow board") + th.crumb.Render("  ·  ") + m.fullTabs(viewSwim)
	right := th.crumb.Render(fmt.Sprintf("%d bands · %d tasks  ·  ", len(l.Bands), l.Tasks)) +
		th.accent.Render("⟨SWIM⟩") + th.dim.Render("  ·  ? help")
	return joinEnds(left, right, m.w)
}

func (m *Model) swimHeader(l *swimLayout, clipped bool) string {
	th := m.th
	left := th.peekHdr.Render("grouped by "+l.Axis.String()) +
		th.dim.Render("  ·  scope ") + th.chipAlt.Render(m.swimScopeName())
	if !l.All {
		left += th.dim.Render(" (done dropped)")
	}

	var bits []string
	// Placements, not tasks: a task carrying two repos is drawn in two bands,
	// and reporting the difference is the only way a band's counts add up to
	// something a reader can check. Said only when the two differ.
	if l.Placed != l.Tasks {
		bits = append(bits, fmt.Sprintf("%d placements", l.Placed))
	}
	if l.Clipped() {
		bits = append(bits, th.warn.Render(fmt.Sprintf("lanes 1-%d of %d — the rest need a wider terminal",
			len(l.Lanes), l.NLane)))
	}
	// The hidden count is an aggregate claim ABOUT THE FILTER, so it must not
	// be made from a verdict the store refused — the dep map's rule, and its
	// words.
	if m.qErr != "" {
		bits = append(bits, th.warn.Render("filter refused — this count is from the last good verdict"))
	} else if hidden := m.swimHiddenCount(l); hidden > 0 {
		bits = append(bits, th.warn.Render(fmt.Sprintf("%d hidden by the filter", hidden)))
	}
	if clipped {
		bits = append(bits, th.dim.Render("^u/^d page"))
	}
	return joinEnds(left, th.dim.Render(strings.Join(bits, " · ")), m.w)
}

func (m *Model) swimScopeName() string {
	if m.swimAll {
		return "all"
	}
	return "open"
}

// swimHiddenCount counts the TASKS the filter would hide, over the same set
// the title bar's count is drawn from. Not placements over the drawn cells: a
// two-repo task is in two bands, and counting it twice put a number beside
// "25 tasks" that no other number in the frame could be reconciled with. Not
// m.b.Tasks() either — that holds the done lane the scope just dropped.
func (m *Model) swimHiddenCount(l *swimLayout) int {
	n := 0
	for id := range l.tasks {
		if m.taskHidden(id) {
			n++
		}
	}
	return n
}

// ---- lines ------------------------------------------------------------------

// swimRow composes ONE line from a rail and one segment per drawn lane. Every
// line in this view goes through it, so a segment can never be one cell wide
// in the header and another width in the rows below it.
func (m *Model) swimRow(l *swimLayout, rail string, cells []string) string {
	out := pad(rail, l.RailW)
	gap := strings.Repeat(" ", swimLaneGap)
	for i := range l.Lanes {
		seg := ""
		if i < len(cells) {
			seg = cells[i]
		}
		out += gap + pad(seg, l.ColW)
	}
	return out
}

// swimBarRows is the sticky lane bar: the lane vocabulary and the whole
// population's count in each, then a rule. It never scrolls — the columns are
// what the numbers below them mean, and a grid whose headings scroll away
// stops being readable exactly when it gets long enough to need them.
func (m *Model) swimBarRows(l *swimLayout) []string {
	th := m.th
	cells := make([]string, 0, len(l.Lanes))
	rules := make([]string, 0, len(l.Lanes))
	for i, lane := range l.Lanes {
		n := l.LaneCount[i]
		hdr := th.colHdr
		if i == m.swimLane {
			hdr = th.colHdrOn
		}
		name := th.laneDot(lane).Render(glyphLaneDot) + " " + hdr.Render(lane.DisplayName())
		cells = append(cells, joinEnds(name, th.colCount.Render(fmt.Sprintf("%d", n)), l.ColW))
		rules = append(rules, th.rule.Render(strings.Repeat("─", maxInt(0, l.ColW))))
	}
	return []string{
		m.swimRow(l, th.dim.Render(ansi.Truncate("group / lane", maxInt(1, l.RailW), "…")), cells),
		m.swimRow(l, th.rule.Render(strings.Repeat("─", maxInt(0, l.RailW))), rules),
	}
}

func (m *Model) swimLineText(l *swimLayout, ln swimLine) string {
	switch ln.Kind {
	case swimLineHeader:
		return m.swimHeaderLine(l, ln.Band)
	case swimLineCells:
		return m.swimCellsLine(l, ln.Band, ln.Row)
	}
	return ""
}

// swimHeaderLine is the band's own line. It is IDENTICAL folded and unfolded
// except for the disclosure marker: folding must never move a number, or the
// histogram a reader is comparing down the screen shifts under them.
func (m *Model) swimHeaderLine(l *swimLayout, bi int) string {
	th := m.th
	b := l.Bands[bi]
	sel := m.swimSel == swimKey(b.Key, "")

	fold := swimFoldShut
	if b.Open {
		fold = swimFoldOpen
	}
	gutter := strings.Repeat(" ", swimSelGutter)
	if sel {
		gutter = th.accent.Render("▌") + " "
	}
	head := gutter + th.dim.Render(fold) + " "
	if b.Marker != "" {
		head += th.ok.Render(b.Marker) + " "
	}
	label := th.peekHdr
	if b.Key == "" {
		label = th.muted // the sentinel is an absence, not a name
	}
	// Compose the suffix first and give the label whatever is left: the slice
	// panel's rule, because a hard-coded budget around CJK text lands the
	// truncation on the digits.
	suffix := th.colCount.Render(fmt.Sprintf("%d", b.Total))
	budget := maxInt(1, l.RailW-lg.Width(head)-lg.Width(suffix)-1)
	rail := joinEnds(head+label.Render(ansi.Truncate(b.Label, budget, "…")), suffix, l.RailW)

	cells := make([]string, 0, len(l.Lanes))
	for i := range l.Lanes {
		n := b.Counts[i]
		// A dot, not a 0: an empty cell in a histogram read down the screen
		// has to be quieter than a number, or the zeros are what the eye
		// lands on. The count is right-aligned on a BLANK field rather than a
		// rule — six columns of ── per band is 150 cells of furniture on a
		// frame whose whole content is 57 numbers.
		txt := th.dim.Render("·")
		if n > 0 {
			style := th.colCount
			if i == m.swimLane {
				style = th.accent
			}
			txt = style.Render(fmt.Sprintf("%d", n))
		}
		// Right-aligned flush, the same anchor the lane bar's total uses one
		// line above: a trailing space here put the histogram one cell left of
		// the number it is read against, in every band.
		cells = append(cells, joinEnds("", txt, l.ColW))
	}
	return m.swimRow(l, rail, cells)
}

// swimCellsLine is one row of an unfolded band: the row-th task of every lane
// that has one.
func (m *Model) swimCellsLine(l *swimLayout, bi, row int) string {
	th := m.th
	b := l.Bands[bi]
	// The rail carries the band's continuation rule, so a row three screens
	// below its header still reads as belonging to a band rather than floating.
	rail := strings.Repeat(" ", swimSelGutter) + th.rule.Render("│")
	cells := make([]string, 0, len(l.Lanes))
	for i := range l.Lanes {
		if row >= len(b.Cells[i]) {
			cells = append(cells, "")
			continue
		}
		cells = append(cells, m.swimCell(b.Cells[i][row], swimKey(b.Key, b.Cells[i][row].ID), l.ColW))
	}
	return m.swimRow(l, rail, cells)
}

// swimCell is one task, on ONE line — never a card. At the 240 floor seven
// lanes leave 27 cells a column, where a bordered card would spend 4 on the
// border and truncate a median-82-cell Japanese title to a fifth of itself.
// The full title is uncut in the strip below, which is what strip.go says the
// strip is for.
func (m *Model) swimCell(t *board.Task, key string, w int) string {
	th := m.th
	gutter := strings.Repeat(" ", swimSelGutter)
	if key == m.swimSel {
		gutter = th.accent.Render("▌") + " "
	}
	glyph, styleFor := cardMarker(t, m.g)
	head := gutter + styleFor(th).Render(glyph) + " "

	titleStyle := th.base
	switch {
	case m.g.IsDone(t.ID):
		titleStyle = th.dim
	case m.taskHidden(t.ID):
		// Mute, never drop: the swimlane's counts are a claim about the board,
		// and a query must not be able to empty a band it did not empty.
		titleStyle = th.dim
	}
	// The id chip is the first thing dropped when the column is too narrow to
	// carry both it and a readable title — the graph's degradation ladder, and
	// the same order: identity that a neighbour can supply goes before the
	// words only this row has.
	id := th.chipAlt.Render(t.ID) + " "
	budget := w - lg.Width(head) - lg.Width(id)
	if budget < 8 {
		id = ""
		budget = w - lg.Width(head)
	}
	return head + id + titleStyle.Render(ansi.Truncate(t.Title, maxInt(1, budget), "…"))
}

// ---- navigation -------------------------------------------------------------

// clampSwimSel keeps the cursor on a row that still exists — a scope change, a
// fold or a re-read can take the row away.
func (m *Model) clampSwimSel(l *swimLayout) {
	m.swimLane = clamp(m.swimLane, 0, maxInt(0, len(l.Lanes)-1))
	if _, ok := l.Pos(m.swimSel); ok {
		return
	}
	m.swimSel = l.First()
}

// scrollSwimToSel keeps the selection on screen using the SAME line number the
// renderer places it at, so the scroll can never disagree with the drawing.
func (m *Model) scrollSwimToSel(l *swimLayout, total, canvasH int) int {
	if total <= canvasH {
		return 0
	}
	p, ok := l.Pos(m.swimSel)
	if !ok {
		return clamp(m.swimScroll, 0, total-canvasH)
	}
	line, _ := l.LineOf(m.swimSel)
	// Scrolling up to a band's first task row reveals the band's HEADER too: a
	// cell is read against the band it belongs to, and stopping one line short
	// leaves the counts just off the top. The dep map reveals a cluster's rule
	// the same way. Read from the position the lookup already resolved — a
	// second BandOf call would be a second chance to index a band that is not
	// there.
	top := line
	if b := l.Bands[p.Band]; line == b.Y+1 {
		top = b.Y
	}
	s := m.swimScroll
	if top < s {
		s = top
	}
	if line >= s+canvasH {
		s = line - canvasH + 1
	}
	return clamp(s, 0, total-canvasH)
}

func (m *Model) swimMove(dx, dy int) {
	l := m.swimLay
	if l == nil {
		return
	}
	next, lane := l.step(m.swimSel, m.swimLane, dx, dy)
	if next != m.swimSel || lane != m.swimLane {
		m.swimMoved = true
	}
	m.swimSel, m.swimLane = next, lane
}

// ---- keys -------------------------------------------------------------------

// openSwim switches to the view, seeded on the board's cursor: its band is
// unfolded and the cell is selected, so `W` shows you where you already are
// rather than a screen you have to navigate back into. With no cursor the
// frame opens fully folded, which is the histogram this view exists to give.
func (m *Model) openSwim() {
	m.cancelDrag()
	m.swimScroll = 0
	m.swimMoved = false
	m.view = viewSwim
	if m.swimOpen == nil {
		m.swimOpen = map[string]bool{}
	}
	band, wasOpen := "", false
	if t := m.curTask(); t != nil {
		if i := m.b.LaneIndex(t.Status); i >= 0 {
			m.swimLane = i
		}
		band = swimKeys(t, m.swimAxis)[0]
		wasOpen = m.swimOpen[band]
		m.swimOpen[band] = true
		m.swimSel = swimKey(band, t.ID)
	}
	l := m.buildSwim()
	// Seeding is all-or-nothing. A cursor this pack cannot hold — a done card
	// at the open scope, a card in a lane the width clip dropped — would leave
	// its band unfolded with nothing selected in it, and clampSwimSel then
	// walks the cursor to band 0: the frame opens expanded on a band the reader
	// is not in. Fall back to the band HEADER, which still says where the card
	// is filed and keeps the opening frame the histogram it claims to be.
	if band != "" {
		if _, ok := l.Pos(m.swimSel); !ok {
			if !wasOpen {
				delete(m.swimOpen, band)
			}
			m.swimSel = swimKey(band, "")
			l = m.buildSwim()
		}
	}
	m.swimLay = l
	m.clampSwimSel(l)
	m.noteSwim()
}

func (m *Model) noteSwim() {
	m.note("swimlane — %s down, lanes across · space folds a band · ⏎ slices to it · tab switches the axis · z scope · esc returns",
		m.swimAxis.String())
}

// closeSwim returns to the board, landing the board cursor on the task the
// walk ended on — the contract every full-screen view keeps.
func (m *Model) closeSwim() {
	m.view = viewBoard
	// Only a cursor the USER moved is carried back, and only when it is on a
	// task: a band header names no row the board could select.
	if m.swimMoved && m.swimLay != nil {
		if id := m.swimLay.IDOf(m.swimSel); id != "" {
			// Pin only what the filter would otherwise hide, so a read-only
			// walk leaves no permanent exemption behind.
			if !m.selectID(id, false) {
				m.selectID(id, true)
			}
		}
	}
	m.note("board view — the cursor followed the swimlane")
}

// toggleSwimFold folds or unfolds the band the cursor is in. Folding parks the
// cursor on that band's header: its cells are about to stop existing, and a
// cursor left on one would be clamped to the first band on the next frame,
// which reads as the view scrolling away on its own.
func (m *Model) toggleSwimFold(l *swimLayout) {
	bi := l.BandOf(m.swimSel)
	if bi < 0 {
		return
	}
	b := l.Bands[bi]
	if m.swimOpen == nil {
		m.swimOpen = map[string]bool{}
	}
	if b.Open {
		delete(m.swimOpen, b.Key)
		m.swimSel = swimKey(b.Key, "")
		m.note("folded %s — %d tasks", b.Label, b.Total)
		return
	}
	m.swimOpen[b.Key] = true
	m.swimSel = swimKey(b.Key, "")
	m.note("unfolded %s — %d tasks", b.Label, b.Total)
}

// sliceToSwimBand emits the -q term the band stands for — the box overview's
// drill-down, verbatim, because a band and a slice mean the same thing and a
// second filtering mechanism would be the one this repo keeps refusing to own.
func (m *Model) sliceToSwimBand(l *swimLayout) tea.Cmd {
	bi := l.BandOf(m.swimSel)
	if bi < 0 {
		m.note("no band under the cursor")
		return nil
	}
	b := l.Bands[bi]
	if b.Key == "" {
		// An absence has no `field:value` spelling. Naming the query furrow
		// DOES answer beats issuing one that means something else.
		m.fail("%s is an absence — type `%s` in the filter to see it", b.Label, swimNoTerm(l.Axis))
		return nil
	}
	// selectSlice runs BEFORE the view moves, and its own verdict is read
	// rather than overwritten. It is a radio (re-selecting the active value
	// clears the slice) and it refuses a value with no -q spelling, so the old
	// unconditional "sliced to <term>" said the opposite of what happened in
	// both cases — once with an empty term, because sliceTerm() was "" by then.
	cmd := m.selectSlice(l.Axis, b.Key)
	if m.statusErr {
		return cmd // refused: its reason stands, and the reader stays where they pressed
	}
	m.view = viewBoard
	if m.sliceVal == "" {
		m.note("slice cleared — %s was already the slice · W returns to the swimlane", b.Label)
		return cmd
	}
	m.note("sliced to %s — s reopens the panel · W returns to the swimlane", m.sliceTerm())
	return cmd
}

// cycleSwimAxis moves the GROUPING axis. It never touches m.sliceField: that
// one carries the active filter, and changing a read-only view's grouping must
// not rewrite the query underneath the board.
func (m *Model) cycleSwimAxis(d int) {
	m.swimAxis = sliceField((int(m.swimAxis) + d + int(sliceFieldCount)) % int(sliceFieldCount))
	m.swimLay = nil
	m.swimScroll = 0
	// The fold set is per-axis by construction (its keys are values of the old
	// axis), so it is dropped rather than left to collide: an epic id and a
	// label are both just strings.
	m.swimOpen = map[string]bool{}
	m.swimSel = ""
	m.noteSwim()
}

func (m *Model) onSwimKey(msg tea.KeyPressMsg) tea.Cmd {
	l := m.swimLay
	if l == nil {
		l = m.buildSwim()
		m.swimLay = l
		m.clampSwimSel(l)
	}
	switch {
	// SwimSlice and SwimFold come FIRST: `enter` also matches keys.Move and
	// `space` also matches keys.Peek, and this handler runs before
	// onNormalKey, so local statement order is what decides. The dep map's and
	// the box overview's handlers live by the same rule.
	case key.Matches(msg, m.keys.SwimSlice):
		return m.sliceToSwimBand(l)

	case key.Matches(msg, m.keys.SwimFold):
		m.toggleSwimFold(l)
		m.swimLay = nil

	case key.Matches(msg, m.keys.Cancel):
		if m.fullHelp {
			m.fullHelp = false
			return nil
		}
		m.closeSwim()

	case key.Matches(msg, m.keys.Swim), key.Matches(msg, m.keys.View):
		m.closeSwim()

	case key.Matches(msg, m.keys.Quit):
		return m.quitOrFlush()

	case key.Matches(msg, m.keys.SwimAxis):
		d := 1
		if msg.String() != "tab" {
			d = -1
		}
		m.cycleSwimAxis(d)

	case key.Matches(msg, m.keys.MapScope):
		// Only the scope changes; the next frame rebuilds the pack and
		// clampSwimSel moves the cursor if its row went away.
		m.swimAll = !m.swimAll
		m.swimLay = nil
		m.note("swimlane scope %s", m.swimScopeName())

	case key.Matches(msg, m.keys.Up):
		m.swimMove(0, -1)
	case key.Matches(msg, m.keys.Down):
		m.swimMove(0, +1)
	case key.Matches(msg, m.keys.Left):
		m.swimMove(-1, 0)
	case key.Matches(msg, m.keys.Right):
		m.swimMove(+1, 0)

	case key.Matches(msg, m.keys.Top):
		m.swimSel, m.swimMoved = l.First(), true
	case key.Matches(msg, m.keys.Bottom):
		if len(l.Bands) > 0 {
			m.swimSel, m.swimLane = l.Last(m.swimLane)
			m.swimMoved = true
		}

	case key.Matches(msg, m.keys.PeekScroll):
		// Half a page of ROWS, not of scroll offset: the window is pinned to
		// the cursor on every frame, so nudging the offset alone snaps straight
		// back and the key this view advertises does nothing. The box overview
		// resolves it the same way.
		dir := 1
		if msg.String() != "ctrl+d" {
			dir = -1
		}
		before := m.swimSel
		for i := maxInt(1, m.swimCanvasH()/2); i > 0; i-- {
			at := m.swimSel
			m.swimMove(0, dir)
			if m.swimSel == at {
				break
			}
		}
		if m.swimSel == before {
			m.note("already at the %s of the swimlane", endName(dir))
		}

	case key.Matches(msg, m.keys.Help):
		m.fullHelp = !m.fullHelp
	}
	return nil
}

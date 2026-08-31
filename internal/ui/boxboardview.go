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

// The BOX OVERVIEW: every box on the board at once, grouped by repo.
// boxboard.go decided the geometry; this file paints it.
//
// It negotiates WIDTH like the dep map, not height like the graph, and for the
// same reason: every row is `pad()`-ed to exactly ColW cells, so the columns
// are text throughout and there is no rune grid for a CJK title to shear.
//
// The row grammar is the slice panel's, widened: the SUFFIX is composed first
// and the title gets whatever is left. Progress, the waiting count and the
// stuck marker are the pieces a reader scans down a column, so they are the
// ones that must survive a Japanese title's ellipsis — the exact failure
// slicemode.go records having shipped once.

// boxDepBudget is the share of a row a "←e-a,e-b" tag may take, mirroring the
// dep map's. On the real board four boxes carry such a tag at all, so this
// almost never bites; it exists so the two boxes that do carry one cannot eat
// their own title.
const boxDepBudget = 4

func (m *Model) boxCanvasH() int {
	return maxInt(1, m.h-fullTop-m.stripHeight()-footerH)
}

// boxPopulation is what the overview shows: the open boxes, or everything.
func (m *Model) boxPopulation() []board.EpicInfo {
	if m.boxesAll {
		return m.b.EpicsAll()
	}
	return m.b.Epics()
}

// buildBoxes packs the population for the current width.
//
// Like the dep map's, the key handlers do NOT call this: they read the cached
// m.boxesLay that renderBoxes rewrote on the previous frame. Sound because
// bubbletea calls View() after every Update — and the scope toggle therefore
// changes nothing but the scope, letting the next frame rebuild.
func (m *Model) buildBoxes() *boxLayout {
	return packBoxes(m.boxPopulation(), m.boxesAll, maxInt(1, m.w-2))
}

func (m *Model) renderBoxes() string {
	l := m.buildBoxes()
	m.boxesLay = l
	m.clampBoxesSel(l)

	bands := m.boxBands(l)
	canvasH := m.boxCanvasH()
	m.boxesScroll = clamp(m.boxesScroll, 0, maxInt(0, len(bands)-canvasH))
	m.boxesScroll = m.scrollBoxesToSel(l, len(bands), canvasH)

	shown := bands
	if len(shown) > canvasH {
		shown = shown[m.boxesScroll:minInt(len(bands), m.boxesScroll+canvasH)]
	}
	canvas := make([]string, 0, canvasH)
	for _, s := range shown {
		canvas = append(canvas, " "+pad(s, maxInt(1, m.w-2)))
	}
	for len(canvas) < canvasH {
		canvas = append(canvas, strings.Repeat(" ", maxInt(1, m.w)))
	}

	parts := []string{
		pad(m.boxTitleBar(l), m.w),
		pad(m.boxHeader(l, len(bands) > canvasH), m.w),
		strings.Join(canvas, "\n"),
	}
	if sh := m.stripHeight(); sh > 0 {
		parts = append(parts, m.boxStrip(m.selectedBox(l), sh))
	}
	parts = append(parts, pad(m.statusLine(), m.w))

	frame := m.fitFrame(strings.Join(parts, "\n"))
	// Composed as a string rather than through the compositor, like the graph
	// and the map — so anything that OWNS THE KEYBOARD has to be layered on
	// here explicitly. Unlike those two this view opens one: `m` hands the
	// keyboard to the epic overlay, and without modalLayers the overlay held
	// every keystroke while rendering not one pixel — the exact failure
	// modalLayers exists to have exactly one home for.
	if layers := m.modalLayers(); len(layers) > 0 {
		frame = m.fitFrame(lg.NewCompositor(append(
			[]*lg.Layer{lg.NewLayer(frame).X(0).Y(0).Z(zChrome)}, layers...)...).Render())
	}
	return frame
}

// selectedBox resolves the cursor to its box, nil when the pack is empty.
func (m *Model) selectedBox(l *boxLayout) *board.EpicInfo {
	r := l.Row(m.boxesSel)
	if r == nil {
		return nil
	}
	return m.b.Epic(r.ID)
}

func (m *Model) boxTitleBar(l *boxLayout) string {
	th := m.th
	left := th.title.Render("furrow board") + th.crumb.Render("  ·  ") +
		m.fullTabs(viewBoxes)
	right := th.crumb.Render(fmt.Sprintf("%d repos · %d rows  ·  ",
		len(l.Groups), len(l.Rows))) + th.accent.Render("⟨BOXES⟩") +
		th.dim.Render("  ·  ? help")
	return joinEnds(left, right, m.w)
}

// boxHeader states the scope and the four counts the overview exists to
// produce. Every one is furrow's own verdict summed, never recomputed from
// member tasks: doing that here would be the front-end logic this repo exists
// not to have, and it would disagree with the rows the same frame drew.
func (m *Model) boxHeader(l *boxLayout, clipped bool) string {
	th := m.th
	scope := "open"
	if l.All {
		scope = "open + closed"
	}
	left := th.peekHdr.Render("boxes by repo") + th.dim.Render("  ·  scope ") +
		th.chipAlt.Render(scope)

	active, stuck, waiting, closed, done, total := 0, 0, 0, 0, 0, 0
	seen := map[string]bool{}
	for _, g := range l.Groups {
		for _, e := range g.Boxes {
			if seen[e.ID] {
				continue // a two-repo box is placed twice; counted once
			}
			seen[e.ID] = true
			if e.Active {
				active++
			}
			if e.Stuck {
				stuck++
			}
			if len(e.OpenDeps) > 0 {
				waiting++
			}
			if !e.Closed.IsZero() {
				closed++
			}
			done += e.Done
			total += e.Total
		}
	}
	bits := []string{
		fmt.Sprintf("%d boxes", len(seen)),
		th.ok.Render(fmt.Sprintf("%d active", active)),
		fmt.Sprintf("%d/%d tasks done", done, total),
	}
	if waiting > 0 {
		bits = append(bits, fmt.Sprintf("%d waiting", waiting))
	}
	if stuck > 0 {
		bits = append(bits, th.warn.Render(fmt.Sprintf("%d stuck", stuck)))
	}
	if closed > 0 {
		bits = append(bits, th.dim.Render(fmt.Sprintf("%d closed", closed)))
	}
	if clipped {
		bits = append(bits, th.dim.Render("^u/^d page"))
	}
	return joinEnds(left, th.dim.Render(strings.Join(bits, " · ")), m.w)
}

// boxBands renders the packed grid to one string per screen line. Every column
// contributes exactly ColW cells at every row, so the join is width-exact and
// the columns cannot drift apart.
func (m *Model) boxBands(l *boxLayout) []string {
	if l.Empty() {
		msg := "— no open boxes on this board (z includes the closed ones) —"
		if l.All {
			msg = "— no boxes on this board at all —"
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
	for _, g := range l.Groups {
		for j, line := range m.renderBoxGroup(g, l.ColW) {
			if y := g.Y + j; y < l.H {
				cols[g.Col][y] = line
			}
		}
	}

	gap := strings.Repeat(" ", boxPanelGap)
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

// renderBoxGroup draws one repo: the rule that names it, then its boxes.
func (m *Model) renderBoxGroup(g boxGroup, w int) []string {
	th := m.th
	out := make([]string, 0, g.H)

	name := g.Repo
	if name == boxNoRepo {
		name = boxNoRepoLabel
	}
	label := fmt.Sprintf(" %s  %d ", name, len(g.Boxes))
	if g.Total > 0 {
		label = fmt.Sprintf(" %s  %d · %d/%d ", name, len(g.Boxes), g.Done, g.Total)
	}
	head := th.rule.Render("──") + th.peekHdr.Render(label)
	if n := w - lg.Width(head); n > 0 {
		head += th.rule.Render(strings.Repeat("─", n))
	}
	out = append(out, pad(head, w))
	for _, e := range g.Boxes {
		out = append(out, m.boxRowLine(g.Repo, e, w))
	}
	return out
}

// boxRowLine is one box: selection gutter, lifecycle marker, id, title, and
// the suffix of furrow's derived numbers.
//
// The suffix is measured and subtracted BEFORE the title is truncated. A
// hard-coded budget holds only for single-digit counts with no markers, and
// the slice panel already shipped that bug once — the outer truncate landed on
// the digits and showed a number that was not the box's progress.
func (m *Model) boxRowLine(repo string, e board.EpicInfo, w int) string {
	th := m.th
	sel := boxKey(repo, e.ID) == m.boxesSel

	gutter := strings.Repeat(" ", boxSelGutter)
	if sel {
		gutter = th.accent.Render("▌") + " "
	}

	marker, markerStyle := " ", th.dim
	switch {
	case !e.Closed.IsZero():
		marker, markerStyle = glyphDone, th.dim
	case e.Active:
		marker, markerStyle = glyphEpicActive, th.ok
	case e.Pinned:
		marker, markerStyle = glyphEpicPinned, th.accent
	}

	suffix := fmt.Sprintf(" %d/%d", e.Done, e.Total)
	if n := len(e.OpenDeps); n > 0 {
		suffix += fmt.Sprintf(" →%d", n)
	}
	if e.Stuck {
		suffix += " !"
	}
	styled := th.muted.Render(suffix)
	if e.Stuck {
		styled = th.muted.Render(suffix[:len(suffix)-1]) + th.warn.Render("!")
	}

	inner := maxInt(8, w-boxSelGutter)
	tag := m.boxDepTag(e.Deps, inner/boxDepBudget)

	head := markerStyle.Render(marker) + " " + th.chipAlt.Render(e.ID) + " "
	titleStyle := th.base
	if !e.Closed.IsZero() {
		titleStyle = th.dim
	}
	// The title's budget is what the row has left once every fixed piece is
	// measured — display cells, never len().
	budget := maxInt(1, inner-lg.Width(head)-lg.Width(suffix)-lg.Width(tag))
	title := titleStyle.Render(ansi.Truncate(e.Title, budget, "…"))
	return pad(gutter+joinEnds(head+title, tag+styled, inner), w)
}

// boxDepTag is blockerTag's epic half. It may NOT reuse blockerTag: that one
// colours by the TASK graph, where an epic id is never Known — so every one of
// the board's four epic edges rendered as an unresolved defect, which is the
// one thing the tag's own contract says it may never do.
//
// The ladder is furrow's three dep states, worst first: an id no read serves
// is what furrow lints epic-dep-missing at severity ERROR; a resolvable box
// that is still open is a live wait; everything else is settled.
func (m *Model) boxDepTag(ids []string, budget int) string {
	tag, shown := depTag(ids, budget)
	if shown == 0 {
		return ""
	}
	style := m.th.dim
	for _, id := range ids[:shown] {
		de := m.b.Epic(id)
		switch {
		case de == nil:
			return m.th.danger.Render(tag)
		case de.Closed.IsZero():
			style = m.th.warn
		}
	}
	return style.Render(tag)
}

// boxStrip is the detail strip in its box shape. taskStrip cannot serve here —
// a box has no lane, no checklist and no due — and the labelling problem the
// strip exists for is the same one: a row can only show a truncated title.
func (m *Model) boxStrip(e *board.EpicInfo, h int) string {
	th := m.th
	inner := maxInt(10, m.w-4)
	if e == nil {
		return th.peek.Width(m.w).Height(h).Render(th.dim.Render("no box selected"))
	}

	// Capped like taskStrip, and for the reason its comment records: Height()
	// is a MINIMUM in lipgloss, so an unbounded body grows past the band and
	// fitFrame clips the status line off the bottom instead. A long goal on a
	// 240-cell board is enough to do it.
	body := maxInt(0, h-2)
	rows := make([]string, 0, body)
	push := func(lines ...string) {
		for _, l := range lines {
			if len(rows) < body {
				rows = append(rows, l)
			}
		}
	}

	head := th.chipAlt.Render(e.ID) + " " + th.base.Render(e.Title)
	if !e.Closed.IsZero() {
		head += th.dim.Render("  closed " + e.Closed.Local().Format("2006-01-02"))
	}
	push(pad(head, inner))
	if e.Goal != "" {
		for _, l := range wrapLines("goal "+e.Goal, inner) {
			push(th.muted.Render(pad(l, inner)))
		}
	}

	meta := []string{fmt.Sprintf("%d/%d done", e.Done, e.Total)}
	if e.Active {
		meta = append(meta, th.ok.Render("active"))
	}
	if e.Standing {
		meta = append(meta, "standing")
	}
	if e.Pinned {
		meta = append(meta, "pinned")
	}
	if e.Stuck {
		meta = append(meta, th.warn.Render("STUCK"))
	}
	if len(e.Repos) > 0 {
		meta = append(meta, "repos "+strings.Join(e.Repos, ","))
	}
	if len(e.Labels) > 0 {
		meta = append(meta, "labels "+strings.Join(e.Labels, ","))
	}
	if keys := e.MetaKeys(); len(keys) > 0 {
		meta = append(meta, "meta "+strings.Join(keys, ","))
	}
	push(strings.Split(th.muted.Render(wrapJoin(meta, " · ", inner)), "\n")...)

	// The dep edges resolved, in the peek's own three words — this view is
	// where the board's four epic edges are actually visible, so it must not
	// invent a fourth vocabulary for them.
	if len(e.Deps) > 0 {
		open := make(map[string]bool, len(e.OpenDeps))
		for _, d := range e.OpenDeps {
			open[d] = true
		}
		parts := []string{"waits on"}
		for _, d := range e.Deps {
			de := m.b.Epic(d)
			switch {
			case open[d] && de == nil:
				parts = append(parts, d)
			case open[d]:
				parts = append(parts, fmt.Sprintf("%s (%d/%d) %s", d, de.Done, de.Total, de.Title))
			case de == nil:
				parts = append(parts, d+" (missing)")
			case !de.Closed.IsZero():
				parts = append(parts, fmt.Sprintf("%s (%d/%d) %s (closed)", d, de.Done, de.Total, de.Title))
			default:
				parts = append(parts, d+" (satisfied)")
			}
		}
		push(strings.Split(th.muted.Render(wrapJoin(parts, " · ", inner)), "\n")...)
	}
	return th.peek.Width(m.w).Height(h).Render(strings.Join(rows, "\n"))
}

func (m *Model) clampBoxesSel(l *boxLayout) {
	if l.Row(m.boxesSel) != nil {
		return
	}
	m.boxesSel = l.First()
}

func (m *Model) scrollBoxesToSel(l *boxLayout, total, canvasH int) int {
	if total <= canvasH {
		return 0
	}
	r := l.Row(m.boxesSel)
	if r == nil {
		return clamp(m.boxesScroll, 0, total-canvasH)
	}
	// Scrolling UP to a group's FIRST row reveals its rule too: a row is read
	// against the repo it belongs to, and stopping one line short leaves the
	// header just off the top — the dep map's own lesson.
	top := r.Y
	if g := l.Groups[r.Group]; r.Y == g.Y+boxPanelHdr {
		top = g.Y
	}
	s := m.boxesScroll
	if top < s {
		s = top
	}
	if r.Y >= s+canvasH {
		s = r.Y - canvasH + 1
	}
	return clamp(s, 0, total-canvasH)
}

// openBoxes enters the overview, landing the cursor on the first ACTIVE box in
// row order when it can find one — that is the row a session opens this view to
// look at. With several repos active it is simply the alphabetically first;
// picking "the right one" would need a scope this view does not have.
func (m *Model) openBoxes() {
	m.cancelDrag()
	m.boxesScroll = 0
	m.view = viewBoxes
	l := m.buildBoxes()
	m.boxesLay = l
	if l.Row(m.boxesSel) == nil {
		m.boxesSel = l.First()
		for _, r := range l.Rows {
			if e := m.b.Epic(r.ID); e != nil && e.Active {
				m.boxesSel = r.Key
				break
			}
		}
	}
	m.note("box overview — every box by repo · ⏎ slices the board to one · m manages it · z scope · esc returns")
}

// closeBoxes returns to the board WITHOUT touching the card cursor. The dep
// map carries its selection back because its rows ARE tasks; a box is not a
// card, so there is nothing to follow — and the way to go from a box to its
// tasks is ⏎, which slices.
func (m *Model) closeBoxes() {
	m.view = viewBoard
	m.note("board view")
}

// drillIntoBox is the whole drill-down: it emits the slice term the panel
// already emits. No second filtering mechanism — a box's tasks are
// `-q epic:<id>`, which furrow evaluates, and this view has no business
// owning a query. It works on a CLOSED box too, which is one of the reasons
// the closed population earns its place.
func (m *Model) drillIntoBox(l *boxLayout) tea.Cmd {
	r := l.Row(m.boxesSel)
	if r == nil {
		m.note("no box under the cursor")
		return nil
	}
	m.view = viewBoard
	cmd := m.selectSlice(sliceEpic, r.ID)
	m.note("sliced to %s — the panel's epic axis holds it · s reopens the panel", r.ID)
	return cmd
}

func (m *Model) onBoxesKey(msg tea.KeyPressMsg) tea.Cmd {
	l := m.boxesLay
	if l == nil {
		l = m.buildBoxes()
		m.boxesLay = l
	}
	switch {
	// Commit and EpicEdit come FIRST: `enter` also matches keys.Move and `m`
	// also matches keys.EpicEdit's sibling binding, and this handler runs
	// before onNormalKey, so local order is what decides. The dep map's
	// handler lives with the same rule.
	case key.Matches(msg, m.keys.BoxSlice):
		return m.drillIntoBox(l)

	case key.Matches(msg, m.keys.EpicEdit):
		r := l.Row(m.boxesSel)
		if r == nil {
			m.note("no box under the cursor")
			return nil
		}
		m.enterEpic(r.ID)

	case key.Matches(msg, m.keys.Cancel):
		if m.fullHelp {
			m.fullHelp = false
			return nil
		}
		m.closeBoxes()

	case key.Matches(msg, m.keys.Boxes), key.Matches(msg, m.keys.View):
		m.closeBoxes()

	case key.Matches(msg, m.keys.Quit):
		return m.quitOrFlush()

	case key.Matches(msg, m.keys.MapScope):
		// Only the scope changes; the next frame rebuilds the pack, and
		// clampBoxesSel moves the cursor if its row went away.
		m.boxesAll = !m.boxesAll
		m.boxesLay = nil

	case key.Matches(msg, m.keys.Up):
		m.boxesSel = l.step(m.boxesSel, 0, -1)
	case key.Matches(msg, m.keys.Down):
		m.boxesSel = l.step(m.boxesSel, 0, +1)
	case key.Matches(msg, m.keys.Left):
		m.boxesSel = l.step(m.boxesSel, -1, 0)
	case key.Matches(msg, m.keys.Right):
		m.boxesSel = l.step(m.boxesSel, +1, 0)

	case key.Matches(msg, m.keys.Top):
		m.boxesSel = l.First()
	case key.Matches(msg, m.keys.Bottom):
		if n := len(l.Rows); n > 0 {
			m.boxesSel = l.Rows[n-1].Key
		}

	case key.Matches(msg, m.keys.PeekScroll):
		// Half a page of ROWS, not of scroll offset: the window is pinned to
		// the cursor by scrollBoxesToSel on every frame, so nudging the offset
		// alone snaps straight back and the key this view's own header
		// advertises does nothing. The dep map resolves it the same way.
		dir := 1
		if msg.String() != "ctrl+d" {
			dir = -1
		}
		before := m.boxesSel
		for i := maxInt(1, m.boxCanvasH()/2); i > 0; i-- {
			at := m.boxesSel
			m.boxesSel = l.step(m.boxesSel, 0, dir)
			if m.boxesSel == at {
				break
			}
		}
		if m.boxesSel == before {
			m.note("already at the %s of this column", endName(dir))
		}

	case key.Matches(msg, m.keys.Help):
		m.fullHelp = !m.fullHelp
	}
	return nil
}

package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The slice panel (t-6ad9): GitHub Projects' "Slice by" pane, for the two
// axes Projects #5 actually declared (repo, label) plus furrow's own epic —
// whose rows carry the progress/stuck the store already computes, which GH
// has no analogue for.
//
// A slice is nothing but an issued -q term: selecting a row ANDs
// `repo:x` / `label:y` / `epic:e-…` into the active query (GH: "works with
// the current filter applied to your view"), so the panel can never disagree
// with the filter bar about what filtering MEANS. The user's typed query and
// the slice are held separately — switching slices never edits the typed
// text.

type sliceField int

const (
	sliceRepo sliceField = iota
	sliceLabel
	sliceEpic
	sliceFieldCount
)

func (f sliceField) String() string {
	return [...]string{"repo", "label", "epic"}[f]
}

const (
	// 32 costs the board no LANE: boardCols derives the lane count from
	// colMinW+colGap, so at the 240-column floor an inset of 27, 29 or 33 all
	// leave 8 lane slots and only an inset of 35 drops one. It is not free —
	// the lanes share what is left, so at 240 each card column loses one cell
	// (33 -> 32) and the table's title column 109 -> 103. Of the six cells two
	// pay for the lifecycle column below, so no row's title budget regresses,
	// and four buy back what the measured suffix eats.
	slicePanelW = 32
	sliceInsetW = slicePanelW + 1
	sliceRowTop = boardTop + 3 // panel header + axis line + scope line
	// sliceMarkW is the lifecycle column: one glyph plus its separating space.
	sliceMarkW = 2
	// sliceWrapCap is the region's line capacity below which a row stays on
	// ONE line however long its title is. A region of 4 lines is the smallest
	// that can hold a two-line row whole even with both indicators reserved
	// (lineCap = capacity-2), and a row you can only ever see the head of is
	// worse than a truncated one — the cursor could never read its own
	// selection. Below this the panel is exactly what it was before wrapping.
	sliceWrapCap = 4
)

// sliceRow is one selectable value: value is what gets ANDed into the query,
// lines is the row's PLAIN text as the frame draws it — one line, or two when
// an epic title did not fit — and the fields under it are those same lines'
// segments, so the renderer styles them without composing the text twice.
//
// len(lines) is the row's HEIGHT, and it is the number every part of the
// panel's geometry counts in. Nothing outside sliceRows may compose row text.
type sliceRow struct {
	value string // the -q value; selection identity
	lines []string
	// The epic axis' segments; mark is "" on the axes that have no lifecycle.
	mark   string // the leading lifecycle glyph, already padded to sliceMarkW
	title  string // the first line's title, truncated only when there is no second
	tail   string // the title's continuation on line two, "" when it fit on one
	suffix string // furrow's derived numbers, never truncated
	closed bool
}

// text is what the row SAYS, independent of how many lines it takes.
func (r sliceRow) text() string { return strings.Join(r.lines, " ") }

// sliceInset is how far the board shifts right while the panel is up.
func (m *Model) sliceInset() int {
	if m.sliceOpen {
		return sliceInsetW
	}
	return 0
}

// sliceTerm is the -q term the current selection stands for, "" when none.
func (m *Model) sliceTerm() string {
	if m.sliceVal == "" {
		return ""
	}
	return m.sliceField.String() + ":" + quoteQVal(m.sliceVal)
}

// quoteQVal wraps a -q value in double quotes when it contains a character
// furrow's lexer would reinterpret: ASCII whitespace splits terms (a bare
// `label:needs review` becomes TWO terms — exit 0, empty result, no warning)
// and a comma OR-splits (`label:a,b` answers a BROADER query). Both verified
// against the real binary, including that quoting suppresses each, and that
// U+3000/NBSP do NOT split (so they need no quoting). Values containing a
// double quote cannot be expressed at all and are refused upstream in
// selectSlice.
func quoteQVal(v string) string {
	if strings.ContainsAny(v, " \t\r\n,") {
		return `"` + v + `"`
	}
	return v
}

// toggleSlice is the `s` key: closed → open with the keyboard; focused →
// close (the SELECTION persists — GH's "No slicing" is an explicit act, not
// a side-effect of hiding the panel); open-but-unfocused → focus.
func (m *Model) toggleSlice() {
	// Opening the panel re-insets every column; a drag surviving that shift
	// would drop a panel's width away from the pointer (observed: the release
	// committed into a lane the pointer never visited).
	m.cancelDrag()
	switch {
	case !m.sliceOpen:
		m.sliceOpen = true
		m.mode = modeSlice
		m.noteSliceAxis()
	case m.mode == modeSlice:
		m.sliceOpen = false
		m.mode = modeNormal
	default:
		m.mode = modeSlice
		m.noteSliceAxis()
	}
	m.relayout()
}

// noteSliceAxis states the panel's keys FOR THE CURRENT AXIS. The panel is a
// modal, so `?` cannot be typed inside it and HelpSections deliberately omits
// the modals — this note is the only surface that can advertise m/A, and the
// epic axis is the only one where they mean anything. Called on open AND on
// every axis switch, or it goes stale on the first tab.
func (m *Model) noteSliceAxis() {
	if m.statusErr {
		return // never over-write a refusal nobody has read yet
	}
	if m.sliceField == sliceEpic {
		scope := "open only"
		if m.sliceEpicAll {
			scope = "open + closed"
		}
		m.note("slice by epic (%s) — tab switches the axis · ⏎ slices · m manages the box · A new box · z scope · esc leaves", scope)
		return
	}
	m.note("slice by %s — tab switches the axis · ⏎ slices · esc leaves the panel", m.sliceField)
}

func (m *Model) onSliceKey(msg tea.KeyPressMsg) tea.Cmd {
	rows := m.sliceRows()
	switch {
	case key.Matches(msg, m.keys.Quit):
		// The panel is a picker, not a text input: q quits here exactly as
		// the footer advertises (ctrl+c rides the same binding).
		return m.quitOrFlush()

	case key.Matches(msg, m.keys.Cancel):
		m.mode = modeNormal // the panel and the slice both stay

	case key.Matches(msg, m.keys.Slice):
		m.toggleSlice()

	case key.Matches(msg, m.keys.Up):
		if len(rows) > 0 {
			m.sliceIdx = (m.sliceIdx + len(rows) - 1) % len(rows)
			m.ensureSliceVisible()
		}
	case key.Matches(msg, m.keys.Down):
		if len(rows) > 0 {
			m.sliceIdx = (m.sliceIdx + 1) % len(rows)
			m.ensureSliceVisible()
		}

	case key.Matches(msg, m.keys.NextCol), key.Matches(msg, m.keys.Right):
		return m.cycleSliceField(+1)
	case key.Matches(msg, m.keys.PrevCol), key.Matches(msg, m.keys.Left):
		return m.cycleSliceField(-1)

	// 178 boxes on the real board, in a 32-cell column: without these the only
	// way to the far end of the list is holding j.
	case key.Matches(msg, m.keys.Top):
		if len(rows) > 0 {
			m.sliceIdx = 0
			m.ensureSliceVisible()
		}
	case key.Matches(msg, m.keys.Bottom):
		if len(rows) > 0 {
			m.sliceIdx = len(rows) - 1
			m.ensureSliceVisible()
		}

	// Both epic keys are answered on EVERY axis, not bound only on the epic
	// one: onSliceKey has no default case, so an axis-conditional binding would
	// be a silent dead key on repo/label — the failure the ^d/^u fix (t-84r1)
	// wrote the rule about. A key that cannot act says why.
	case key.Matches(msg, m.keys.EpicEdit):
		if m.sliceField != sliceEpic {
			m.note("m manages a BOX — switch to the epic axis with tab")
			return nil
		}
		if m.sliceIdx >= len(rows) {
			m.note("no box under the cursor")
			return nil
		}
		m.enterEpic(rows[m.sliceIdx].value)
	case key.Matches(msg, m.keys.EpicNew):
		if m.sliceField != sliceEpic {
			m.note("A creates a BOX — switch to the epic axis with tab")
			return nil
		}
		return m.enterEpicNew()

	// The same key and the same word the dep map's scope toggle uses: one
	// meaning across two surfaces. It exists so a box closed in an earlier
	// session can be found at all — `epic reopen` lives behind `m`, and the
	// default population deliberately hides its target.
	case key.Matches(msg, m.keys.MapScope):
		if m.sliceField != sliceEpic {
			m.note("z widens the BOX list — switch to the epic axis with tab")
			return nil
		}
		m.sliceEpicAll = !m.sliceEpicAll
		m.sliceIdx = 0
		m.ensureSliceVisible()
		m.noteSliceAxis()

	case key.Matches(msg, m.keys.Commit), key.Matches(msg, m.keys.Check):
		if m.sliceIdx < len(rows) {
			return m.selectSlice(m.sliceField, rows[m.sliceIdx].value)
		}
	}
	return nil
}

// cycleSliceField switches the axis. An active selection on the old axis is
// cleared — a repo slice makes no claim about labels.
func (m *Model) cycleSliceField(d int) tea.Cmd {
	m.sliceField = sliceField((int(m.sliceField) + d + int(sliceFieldCount)) % int(sliceFieldCount))
	m.sliceIdx, m.sliceOff = 0, 0
	if m.sliceVal != "" {
		m.sliceVal = ""
		m.pinned = map[string]bool{} // see selectSlice
		cmd := m.refire(m.curTask(), false)
		m.noteSliceAxis()
		return cmd
	}
	m.noteSliceAxis()
	return nil
}

// selectSlice applies a row: selecting the active value again un-slices
// (radio semantics — GH's panel shows one value at a time).
func (m *Model) selectSlice(f sliceField, val string) tea.Cmd {
	if strings.Contains(val, `"`) {
		// furrow's -q quoting has no escape, so this value has no spelling.
		// Refusing loudly beats issuing a query that means something else.
		m.fail("cannot slice to %q — a double quote has no -q spelling", val)
		return nil
	}
	if m.sliceField == f && m.sliceVal == val {
		m.sliceVal = ""
		m.note("slice cleared")
	} else {
		m.sliceField, m.sliceVal = f, val
		m.note("sliced to %s", m.sliceTerm())
	}
	// A slice change is a new view: pins were jump/add artifacts of the old
	// one, and under a slice-only filter no other gesture can clear them.
	m.pinned = map[string]bool{}
	return m.refire(m.curTask(), false)
}

// sliceRows builds the current axis' value list from the board snapshot.
func (m *Model) sliceRows() []sliceRow {
	counts := map[string]int{}
	var out []sliceRow
	switch m.sliceField {
	case sliceRepo:
		for _, t := range m.b.Tasks() {
			for _, r := range t.Repos {
				counts[r]++
			}
		}
		for _, r := range m.repoVocab() {
			short := r
			if i := strings.LastIndex(r, "/"); i >= 0 {
				short = r[i+1:]
			}
			out = append(out, sliceRow{value: r,
				lines: []string{fmt.Sprintf("%s %d", short, counts[r])}})
		}
	case sliceLabel:
		for _, t := range m.b.Tasks() {
			for _, l := range t.Labels {
				counts[l]++
			}
		}
		for _, l := range m.labelVocab() {
			out = append(out, sliceRow{value: l,
				lines: []string{fmt.Sprintf("%s %d", l, counts[l])}})
		}
	case sliceEpic:
		boxes := m.b.Epics()
		if m.sliceEpicAll {
			boxes = m.b.EpicsAll()
		}
		for _, e := range boxes {
			// The lifecycle LEADS the row, at a fixed column, and is drawn
			// before the title is ever cut — boxRowLine's grammar. Riding the
			// suffix, `v` landed after the title's ellipsis, which is the one
			// place on the row a reader scanning the column never looks.
			// furrow clears `active` when it closes a box, so the two never
			// collide and the ladder can be a switch.
			//
			// boxRowLine's ladder has a third rung, `◆ pinned`. This one
			// deliberately does not: the overview's column can afford to be
			// exclusive, while the panel is the only surface `z` reaches and
			// has to be able to say `v` and `◆` at once. So pinned stays in
			// the suffix here, and an open pinned box carries `◆` in the
			// panel's suffix where the overview carries it in its column.
			mark := " "
			switch {
			case !e.Closed.IsZero():
				mark = glyphDone
			case e.Active:
				mark = glyphEpicActive // furrow brief's own marker for the box a repo works out of
			}
			// Pinned stays ADDITIVE at the head of the suffix rather than
			// joining the ladder: furrow keeps `pinned` when it closes a box
			// (measured on v5.0.0 — `epic done` on a pinned box answers
			// changed:[closed] and after.pinned true), so `v` + `◆` is a
			// legitimate pair and one exclusive column could not say both.
			//
			// Build the suffix FIRST and give the title whatever is left. The
			// old `slicePanelW-11` hard-coded a 7-cell suffix budget, which
			// only holds for single-digit counts with no stuck marker: the
			// renderer's outer truncate to w-4 then landed on the digits, so
			// the panel showed a number that was not the epic's progress and
			// ate the stuck marker that glossary.md makes part of the row
			// ("epic 行は store の progress/stuck つき").
			// Measure the composed pieces; never hard-code a cell budget
			// around CJK text.
			suffix := ""
			if e.Pinned {
				suffix += " " + glyphEpicPinned
			}
			suffix += fmt.Sprintf(" %d/%d", e.Done, e.Total)
			// The epic-dep readout: →N = this box waits on N still-open
			// boxes ("open after those close"), straight off furrow's
			// derived open_deps. Informational like the edge itself —
			// furrow warns and proceeds — so it is dim data, not a red
			// refusal; the peek resolves the ids.
			if n := len(e.OpenDeps); n > 0 {
				suffix += fmt.Sprintf(" →%d", n)
			}
			if e.Stuck {
				suffix += " !"
			}
			// A title that does not fit takes a SECOND LINE rather than an
			// ellipsis at 20 cells. Measured on the real board (2026-09-11):
			// of the 133 open boxes only 26 wrap, so the list pays 1.20 lines
			// per row on average, and the 34 boxes that are not one of the
			// three reserved names go from 2 of 34 readable whole to 13 —
			// every one of them showing its `<repo>: <headline>` head, which
			// is what tells two boxes apart.
			//
			// Line 1 is the title's alone: the suffix moves to the last line,
			// so furrow's numbers can no longer squeeze a title to nothing.
			avail := slicePanelW - 4 - sliceMarkW
			one := maxInt(4, avail-lg.Width(suffix))
			title, tail := e.Title, ""
			switch {
			case lg.Width(title) <= one:
				// It fits: one line, and the suffix rides it.
			case m.h-footerH-sliceRowTop < sliceWrapCap:
				// No room to draw a second line whole (see sliceWrapCap).
				title = ansi.Truncate(e.Title, one, "…")
			default:
				// Split on the CELL boundary, not a word one: an exact prefix
				// and the remainder wastes no cells, and TrimPrefix is safe
				// because ansi.Truncate with no ellipsis returns a prefix.
				title = ansi.Truncate(e.Title, avail, "")
				tail = ansi.Truncate(strings.TrimPrefix(e.Title, title), one, "…")
			}
			row := sliceRow{
				value:  e.ID,
				mark:   mark + " ",
				title:  title,
				tail:   tail,
				suffix: suffix,
				closed: !e.Closed.IsZero(),
			}
			if tail == "" {
				row.lines = []string{row.mark + title + suffix}
			} else {
				// The continuation hangs under the title, clear of the mark
				// column, so a two-line row cannot be read as two rows.
				row.lines = []string{row.mark + title, strings.Repeat(" ", sliceMarkW) + tail + suffix}
			}
			out = append(out, row)
		}
	}
	return out
}

// sliceGeom is THE shared measurement for the panel's value region: the
// renderer, the wheel and the click path all read it, so a click can never
// land on a row the frame is not showing (the previous click path validated
// against the FULL list and sliced the board from the "+N more" line and
// even the status line).
//
// A row is no longer one line — an epic row whose title wrapped is two — so
// off/window count ROWS while lineCap counts LINES, and the two must be
// produced together or the frame and the click path can disagree by a line.
type sliceGeom struct {
	off, window int  // the first row drawn, and how many rows follow it
	indicators  bool // the "↑/+N more" lines are reserved at both ends
	lineCap     int  // lines the region may draw, indicators already subtracted
}

// sliceViewport measures that region. It is the lines between the 3 header
// rows and the footer; when the list overflows AND there is room, one line at
// each end is reserved for the indicators, whether or not both are needed — a
// fixed shape keeps the y→row mapping trivial. On terminals too short for
// indicators (capacity < 3) the region shows bare rows: still scrollable by
// cursor and wheel, never mapped past what is rendered.
//
// off is a pure clamp of m.sliceOff — the cursor does NOT drag the window
// here (a wheel-scrolled panel away from its cursor is a legitimate state);
// ensureSliceVisible re-pulls it on cursor MOVEMENT only.
func (m *Model) sliceViewport(rows []sliceRow) sliceGeom {
	capacity := m.h - footerH - sliceRowTop
	if capacity <= 0 {
		// The terminal is too short to render ANY value row — flooring the
		// capacity at 1 here fabricated a clickable row on top of the
		// footer (the invariant test caught it at h ≤ 10).
		return sliceGeom{}
	}
	total := 0
	for _, r := range rows {
		total += len(r.lines)
	}
	if total <= capacity {
		return sliceGeom{window: len(rows), lineCap: capacity}
	}
	g := sliceGeom{lineCap: capacity}
	if capacity >= 3 {
		g.indicators = true
		g.lineCap = capacity - 2
	}
	// The furthest the window may scroll is the offset whose rows still reach
	// the end of the list. With uniform rows that was rowCount-window; with
	// mixed heights it has to be walked from the back.
	maxOff, used := maxInt(0, len(rows)-1), 0
	for i := len(rows) - 1; i >= 0; i-- {
		if used+len(rows[i].lines) > g.lineCap {
			break
		}
		used += len(rows[i].lines)
		maxOff = i
	}
	g.off = clamp(m.sliceOff, 0, maxOff)
	// How many rows fit from there. At least one, even when a single row is
	// taller than the region: it renders clipped rather than not at all, and
	// the click path below maps only the lines that were drawn.
	used = 0
	for i := g.off; i < len(rows); i++ {
		if used+len(rows[i].lines) > g.lineCap && g.window > 0 {
			break
		}
		used += len(rows[i].lines)
		g.window++
	}
	return g
}

// ensureSliceVisible scrolls the panel window so the cursor row is shown.
// Called from the paths that MOVE THE CURSOR, mirroring ensureVisible's
// contract for board columns.
func (m *Model) ensureSliceVisible() {
	rows := m.sliceRows()
	if m.sliceIdx < m.sliceOff {
		m.sliceOff = m.sliceIdx
	}
	// Mixed row heights make "how far down must the window start" a walk, not
	// a subtraction: step the offset until the cursor row is inside the
	// window. Bounded by the row count — a render loop must terminate even if
	// the arithmetic above is ever wrong.
	for n := 0; n <= len(rows); n++ {
		g := m.sliceViewport(rows)
		m.sliceOff = g.off
		if m.sliceIdx < g.off+g.window || g.off >= maxInt(0, len(rows)-1) {
			return
		}
		m.sliceOff = g.off + 1
	}
}

// sliceRowAt maps a frame y to an index into sliceRows, -1 when the line is
// not a value row (chrome, indicators, past the end). Inverse of sliceLayer's
// row placement, walked over the same heights the renderer draws — including
// the clip at lineCap, so the line under a pointer is a row only if the frame
// actually drew it.
func (m *Model) sliceRowAt(y int, rows []sliceRow) int {
	g := m.sliceViewport(rows)
	top := sliceRowTop
	if g.indicators {
		top++ // the "↑ N more" indicator line
	}
	i := y - top
	if i < 0 || i >= g.lineCap {
		return -1
	}
	for r := g.off; r < g.off+g.window && r < len(rows); r++ {
		h := len(rows[r].lines)
		if i < h {
			return r
		}
		i -= h
	}
	return -1
}

// sliceClick is a mouse press inside the panel: a value row selects, the
// axis line cycles, everything else — indicators included — is inert.
func (m *Model) sliceClick(_, y int) tea.Cmd {
	// The axis line is a fixed row, but it must still be RENDERED to be
	// clickable — at h ≤ 7 this y is the help line or past the frame
	// (the one click path not built on sliceViewport).
	if y == boardTop+1 && y < m.h-footerH { // the axis line
		return m.cycleSliceField(+1)
	}
	rows := m.sliceRows()
	i := m.sliceRowAt(y, rows)
	if i < 0 {
		return nil
	}
	m.sliceIdx = i
	m.mode = modeSlice
	return m.selectSlice(m.sliceField, rows[i].value)
}

// sliceScope is the panel's third chrome line: the axis' population, and on
// the epic axis the road to the boxes it is not showing. It replaces a blank
// line, so no y moves and the click path is untouched.
//
// It exists because `z` is otherwise announced only in the note, which the
// next `sliced to …` overwrites — and the box the user wants may well be one
// closed in an earlier session, which the default population hides.
func (m *Model) sliceScope(rowCount int) string {
	switch m.sliceField {
	case sliceEpic:
		open := len(m.b.Epics())
		shut := len(m.b.EpicsAll()) - open
		switch {
		case shut == 0:
			return fmt.Sprintf("%d open", open)
		case m.sliceEpicAll:
			return fmt.Sprintf("%d open · %d closed", open, shut)
		default:
			return fmt.Sprintf("%d open · +%d closed  z", open, shut)
		}
	case sliceLabel:
		return fmt.Sprintf("%d labels", rowCount)
	default:
		return fmt.Sprintf("%d repos", rowCount)
	}
}

// sliceReadout names the row under the cursor IN FULL, for the bottom row.
//
// That row's stated job (README) is to carry what is NOT on the screen, and a
// box title cut at the panel's width is exactly that. The panel is a picker:
// its rows are scanned, so the reading surface is this line — the same split
// strip.go argues for under the full-screen views ("a box or a row can only
// ever show a truncated title; the strip is the answer").
//
// Budget: at the 240-column floor the line leaves ~170 cells once the panel's
// note is measured, and the longest box title on the real board is 145
// (2026-09-11, 178 boxes), so every box reads whole. The caller hands this to
// joinEnds, which truncates the LEFT — so the note, and a refusal, are never
// the half that yields.
//
// It rebuilds the row list (measured 2026-09-11: 0.98ms for 178 boxes, against
// the five rebuilds the panel's own paths already do per event). A frame is
// drawn per message here, not per tick, so this is not the thing to cache.
func (m *Model) sliceReadout() string {
	rows := m.sliceRows()
	if m.sliceIdx >= len(rows) {
		return ""
	}
	th := m.th
	r := rows[m.sliceIdx]
	e := m.b.Epic(r.value)
	if m.sliceField != sliceEpic || e == nil {
		// A repo row shows the basename alone, so this is the one place the
		// whole owner/repo is on screen; a label is its own whole value.
		return th.muted.Render(r.value)
	}
	// furrow's own words for a box, in boxStrip's order — this line must not
	// invent a second vocabulary for the same facts.
	meta := []string{fmt.Sprintf("%d/%d done", e.Done, e.Total)}
	if !e.Closed.IsZero() {
		meta = append(meta, "closed "+e.Closed.In(localZone()).Format("2006-01-02"))
	}
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
	if n := len(e.OpenDeps); n > 0 {
		meta = append(meta, fmt.Sprintf("waits on %d", n))
	}
	if len(e.Repos) > 0 {
		// The one field that separates the reserved boxes: 99 of the real
		// board's 133 open boxes are titled mandate / parking-lot / requests,
		// one per repo (measured 2026-09-11), so for three quarters of the
		// epic axis the repo is the only thing that tells two rows apart.
		meta = append(meta, "repos "+strings.Join(e.Repos, ","))
	}
	return th.chipAlt.Render(e.ID) + " " + th.base.Render(e.Title) +
		th.muted.Render("  "+strings.Join(meta, " · "))
}

// sliceRowBody styles one row's segments. The row's TEXT is composed once, in
// sliceRows, so this may not truncate: the budget was already measured there,
// and cutting here would cut a string that is already styled.
//
// The one path that can hand this an over-wide row is sliceRows' `maxInt(4,
// …)` title floor, which needs a 23-cell suffix — seven-digit counts — to
// bite. There pad() truncates the composed line; it is ANSI-aware, so what is
// lost is the suffix's tail and not the escape sequence around it.
//
// hi is "the cursor or the issued slice is on this row". It wins over the
// closed dim — where you are outranks what the row is — which is why a closed
// row's leading mark is never styled away: that glyph is the signal -plain
// keeps and colour is not.
func (m *Model) sliceRowBody(r sliceRow, li int, style lg.Style, hi bool, w int) string {
	th := m.th
	if r.mark == "" { // repo / label: one flat value, no lifecycle to carry
		return style.Render(ansi.Truncate(r.lines[li], w, "…"))
	}
	markStyle, titleStyle, sufStyle := th.dim, style, th.muted
	switch {
	case r.closed:
		// A finished box recedes WHOLE, so the closed tail of the list reads as
		// one block rather than a column of glyphs. Two deliberate steps past
		// boxRowLine, which dims the title alone: here the suffix dims too (the
		// panel has no id chip to carry the contrast), and so does the stuck
		// marker below — a box furrow reports closed AND stuck (e-6k9x on the
		// real board, 2026-09-11) has nothing left to act on, so one
		// warn-coloured cell in a dim row would be the loudest thing in the
		// list.
		sufStyle = th.dim
		if !hi {
			titleStyle = th.dim
		}
	case strings.HasPrefix(r.mark, glyphEpicActive):
		markStyle = th.ok
	}
	suffix := sufStyle.Render(r.suffix)
	if !r.closed && strings.HasSuffix(r.suffix, glyphWIPOver) {
		suffix = sufStyle.Render(strings.TrimSuffix(r.suffix, glyphWIPOver)) + th.warn.Render(glyphWIPOver)
	}
	if li > 0 {
		// The continuation: a hanging indent where the mark was, the title's
		// rest, and the numbers the first line gave up so the title could have
		// the whole width.
		return strings.Repeat(" ", sliceMarkW) + titleStyle.Render(r.tail) + suffix
	}
	head := markStyle.Render(r.mark) + titleStyle.Render(r.title)
	if r.tail != "" {
		return head // the suffix rides the last line
	}
	return head + suffix
}

// sliceLayer renders the panel: axis header, value rows (cursor while the
// panel holds the keyboard), a right-hand rule to fence it off the board.
func (m *Model) sliceLayer() *lg.Layer {
	th := m.th
	w := slicePanelW
	bot := m.h - footerH
	rows := m.sliceRows()
	g := m.sliceViewport(rows)
	m.sliceOff = g.off

	line := func(s string) string { return pad(s, w) + th.rule.Render("│") }

	var b []string
	b = append(b, line(th.peekHdr.Render("Slice by")))
	var axes []string
	for f := sliceField(0); f < sliceFieldCount; f++ {
		name := f.String()
		if f == m.sliceField {
			axes = append(axes, th.tabOn.Render(name))
		} else {
			axes = append(axes, th.tabOff.Render(name))
		}
	}
	b = append(b, line(strings.Join(axes, th.dim.Render(" │ "))))
	b = append(b, line(th.dim.Render(m.sliceScope(len(rows)))))

	if g.indicators {
		up := ""
		if g.off > 0 {
			up = fmt.Sprintf("  ↑ %d more", g.off)
		}
		b = append(b, line(th.dim.Render(up)))
	}
	drawn := 0
	for i := g.off; i < g.off+g.window && i < len(rows); i++ {
		r := rows[i]
		cursor, style := "  ", th.base
		if m.mode == modeSlice && i == m.sliceIdx {
			cursor, style = "▌ ", th.peekHdr
		}
		sel, hi := "  ", m.mode == modeSlice && i == m.sliceIdx
		if m.sliceVal == r.value {
			sel, style, hi = "● ", th.accent, true
		}
		for li := range r.lines {
			if drawn >= g.lineCap {
				break // the region's clip, the same one sliceRowAt refuses past
			}
			// The cursor bar spans BOTH lines of a wrapped row (it is one row,
			// and a bar on the head alone reads as a one-line row above a
			// stray); the selection mark does not — it names the value once.
			mark := sel
			if li > 0 {
				mark = "  "
			}
			b = append(b, line(cursor+mark+m.sliceRowBody(r, li, style, hi, w-4)))
			drawn++
		}
	}
	if g.indicators {
		down := ""
		if n := len(rows) - (g.off + g.window); n > 0 {
			down = fmt.Sprintf("  +%d more", n)
		}
		b = append(b, line(th.dim.Render(down)))
	}
	for len(b) < bot-boardTop {
		b = append(b, line(""))
	}
	box := strings.Join(b, "\n")
	box = lg.NewStyle().MaxWidth(sliceInsetW).MaxHeight(maxInt(1, bot-boardTop)).Render(box)
	return lg.NewLayer(box).ID("slice").X(0).Y(boardTop).Z(zChrome)
}

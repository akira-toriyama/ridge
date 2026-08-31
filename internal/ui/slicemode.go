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
	slicePanelW = 26 // 24-28 fits even the 240-column floor
	sliceInsetW = slicePanelW + 1
	sliceRowTop = boardTop + 3 // panel header + axis line + blank
)

// sliceRow is one selectable value: term is what gets ANDed into the query,
// display is the human line (short repo, epic progress).
type sliceRow struct {
	value   string // the -q value; selection identity
	display string
}

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
	// would drop 27 cells away from the pointer (observed: the release
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
// every axis switch: written once at open, it went stale on the first tab.
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

	// 111 boxes on the real board, in a 26-cell column: without these the only
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
				display: fmt.Sprintf("%s %d", short, counts[r])})
		}
	case sliceLabel:
		for _, t := range m.b.Tasks() {
			for _, l := range t.Labels {
				counts[l]++
			}
		}
		for _, l := range m.labelVocab() {
			out = append(out, sliceRow{value: l,
				display: fmt.Sprintf("%s %d", l, counts[l])})
		}
	case sliceEpic:
		boxes := m.b.Epics()
		if m.sliceEpicAll {
			boxes = m.b.EpicsAll()
		}
		for _, e := range boxes {
			// Build the suffix FIRST and give the title whatever is left. The
			// old `slicePanelW-11` hard-coded a 7-cell suffix budget, which
			// only holds for single-digit counts with no stuck marker: the
			// renderer's outer truncate to w-4 then landed on the digits, so
			// the panel showed a number that was not the epic's progress and
			// ate the stuck marker that glossary.md makes part of the row
			// ("epic 行は store の progress/stuck つき").
			// Measure the composed pieces; never hard-code a cell budget
			// around CJK text.
			// The lifecycle markers lead the suffix: they are the shortest
			// pieces and the ones the epic overlay acts on, so they must
			// survive the CJK title's ellipsis. ▶ = the box this repo is
			// working out of (furrow's own brief marker), ◆ = pinned.
			suffix := ""
			if !e.Closed.IsZero() {
				// Additive, not exclusive: furrow clears `active` when it
				// closes a box but leaves `pinned` alone (measured on v5.0.0 —
				// `epic done` on a pinned box answers changed:[closed] and
				// after.pinned true), so a closed box can still carry ◆ and the
				// row must be able to say both.
				suffix += " " + glyphDone
			}
			if e.Active {
				suffix += " " + glyphEpicActive
			}
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
			budget := maxInt(4, slicePanelW-4-lg.Width(suffix))
			out = append(out, sliceRow{
				value:   e.ID,
				display: ansi.Truncate(e.Title, budget, "…") + suffix,
			})
		}
	}
	return out
}

// sliceViewport is THE shared measurement for the panel's value region: the
// renderer, the wheel and the click path all use it, so a click can never
// land on a row the frame is not showing (the previous click path validated
// against the FULL list and sliced the board from the "+N more" line and
// even the status line). The region is the lines between the 3 header rows
// and the footer; when the list overflows AND there is room, one line at
// each end is reserved for the "↑/+N more" indicators, whether or not both
// are needed — a fixed shape keeps the y→row mapping trivial. On terminals
// too short for indicators (capacity < 3) the region shows bare rows: still
// scrollable by cursor and wheel, never mapped past what is rendered.
//
// off is a pure clamp of m.sliceOff — the cursor does NOT drag the window
// here (a wheel-scrolled panel away from its cursor is a legitimate state);
// ensureSliceVisible re-pulls it on cursor MOVEMENT only.
func (m *Model) sliceViewport(rowCount int) (off, window int, indicators bool) {
	capacity := m.h - footerH - sliceRowTop
	if capacity <= 0 {
		// The terminal is too short to render ANY value row — flooring the
		// capacity at 1 here fabricated a clickable row on top of the
		// footer (the invariant test caught it at h ≤ 10).
		return 0, 0, false
	}
	if rowCount <= capacity {
		return 0, rowCount, false
	}
	window = capacity
	if capacity >= 3 {
		indicators = true
		window = capacity - 2
	}
	off = clamp(m.sliceOff, 0, rowCount-window)
	return off, window, indicators
}

// ensureSliceVisible scrolls the panel window so the cursor row is shown.
// Called from the paths that MOVE THE CURSOR, mirroring ensureVisible's
// contract for board columns.
func (m *Model) ensureSliceVisible() {
	rows := len(m.sliceRows())
	_, window, _ := m.sliceViewport(rows)
	if m.sliceIdx < m.sliceOff {
		m.sliceOff = m.sliceIdx
	}
	if m.sliceIdx >= m.sliceOff+window {
		m.sliceOff = m.sliceIdx - window + 1
	}
	off, _, _ := m.sliceViewport(rows)
	m.sliceOff = off
}

// sliceRowAt maps a frame y to an index into sliceRows, -1 when the line is
// not a value row (chrome, indicators, past the end). Inverse of sliceLayer's
// row placement, built on the same sliceViewport numbers.
func (m *Model) sliceRowAt(y int, rowCount int) int {
	off, window, indicators := m.sliceViewport(rowCount)
	top := sliceRowTop
	if indicators {
		top++ // the "↑ N more" indicator line
	}
	i := y - top
	if i < 0 || i >= window {
		return -1
	}
	if off+i >= rowCount {
		return -1
	}
	return off + i
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
	i := m.sliceRowAt(y, len(rows))
	if i < 0 {
		return nil
	}
	m.sliceIdx = i
	m.mode = modeSlice
	return m.selectSlice(m.sliceField, rows[i].value)
}

// sliceLayer renders the panel: axis header, value rows (cursor while the
// panel holds the keyboard), a right-hand rule to fence it off the board.
func (m *Model) sliceLayer() *lg.Layer {
	th := m.th
	w := slicePanelW
	bot := m.h - footerH
	rows := m.sliceRows()
	off, window, indicators := m.sliceViewport(len(rows))
	m.sliceOff = off

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
	b = append(b, line(""))

	if indicators {
		up := ""
		if off > 0 {
			up = fmt.Sprintf("  ↑ %d more", off)
		}
		b = append(b, line(th.dim.Render(up)))
	}
	for i := off; i < off+window && i < len(rows); i++ {
		r := rows[i]
		cursor, style := "  ", th.base
		if m.mode == modeSlice && i == m.sliceIdx {
			cursor, style = "▌ ", th.peekHdr
		}
		mark := "  "
		if m.sliceVal == r.value {
			mark, style = "● ", th.accent
		}
		b = append(b, line(cursor+mark+style.Render(ansi.Truncate(r.display, w-4, "…"))))
	}
	if indicators {
		down := ""
		if n := len(rows) - (off + window); n > 0 {
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

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
	return m.sliceField.String() + ":" + m.sliceVal
}

// toggleSlice is the `s` key: closed → open with the keyboard; focused →
// close (the SELECTION persists — GH's "No slicing" is an explicit act, not
// a side-effect of hiding the panel); open-but-unfocused → focus.
func (m *Model) toggleSlice() {
	switch {
	case !m.sliceOpen:
		m.sliceOpen = true
		m.mode = modeSlice
		m.note("slice by %s — tab switches the axis · ⏎ slices · esc leaves the panel", m.sliceField)
	case m.mode == modeSlice:
		m.sliceOpen = false
		m.mode = modeNormal
	default:
		m.mode = modeSlice
	}
	m.relayout()
}

func (m *Model) onSliceKey(msg tea.KeyPressMsg) tea.Cmd {
	rows := m.sliceRows()
	switch {
	case key.Matches(msg, m.keys.Cancel):
		m.mode = modeNormal // the panel and the slice both stay

	case key.Matches(msg, m.keys.Slice):
		m.toggleSlice()

	case key.Matches(msg, m.keys.Up):
		if len(rows) > 0 {
			m.sliceIdx = (m.sliceIdx + len(rows) - 1) % len(rows)
		}
	case key.Matches(msg, m.keys.Down):
		if len(rows) > 0 {
			m.sliceIdx = (m.sliceIdx + 1) % len(rows)
		}

	case key.Matches(msg, m.keys.NextCol), key.Matches(msg, m.keys.Right):
		return m.cycleSliceField(+1)
	case key.Matches(msg, m.keys.PrevCol), key.Matches(msg, m.keys.Left):
		return m.cycleSliceField(-1)

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
	m.sliceIdx = 0
	if m.sliceVal != "" {
		m.sliceVal = ""
		return m.refire(m.curTask(), false)
	}
	return nil
}

// selectSlice applies a row: selecting the active value again un-slices
// (radio semantics — GH's panel shows one value at a time).
func (m *Model) selectSlice(f sliceField, val string) tea.Cmd {
	if m.sliceField == f && m.sliceVal == val {
		m.sliceVal = ""
		m.note("slice cleared")
	} else {
		m.sliceField, m.sliceVal = f, val
		m.note("sliced to %s", m.sliceTerm())
	}
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
		for _, e := range m.b.Epics() {
			d := fmt.Sprintf("%s %d/%d", ansi.Truncate(e.Title, slicePanelW-11, "…"), e.Done, e.Total)
			if e.Stuck {
				d += " !"
			}
			out = append(out, sliceRow{value: e.ID, display: d})
		}
	}
	return out
}

// sliceClick is a mouse press inside the panel: a value row selects, the
// axis line cycles.
func (m *Model) sliceClick(_, y int) tea.Cmd {
	if y == boardTop+1 { // the axis line
		return m.cycleSliceField(+1)
	}
	rows := m.sliceRows()
	i := y - sliceRowTop
	if i < 0 || i >= len(rows) {
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

	visRows := maxInt(0, bot-sliceRowTop-1)
	shown := rows
	if len(shown) > visRows {
		shown = shown[:visRows]
	}
	for i, r := range shown {
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
	if n := len(rows) - len(shown); n > 0 {
		b = append(b, line(th.dim.Render(fmt.Sprintf("  +%d more", n))))
	}
	for len(b) < bot-boardTop {
		b = append(b, line(""))
	}
	box := strings.Join(b, "\n")
	box = lg.NewStyle().MaxWidth(sliceInsetW).MaxHeight(maxInt(1, bot-boardTop)).Render(box)
	return lg.NewLayer(box).ID("slice").X(0).Y(boardTop).Z(zChrome)
}

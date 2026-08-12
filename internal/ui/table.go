package ui

import (
	"fmt"
	"sort"
	"strings"

	"github.com/akira-toriyama/ridge/internal/board"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

// The flat table view, because GitHub Projects has one. It is hand-rolled
// rather than bubbles/v2's table: that widget has no horizontal scroll (its
// Update never feeds the viewport), no sort API, no per-cell styling, and
// fixed int column widths. Its truncation itself is CJK-safe (ansi.Truncate,
// grapheme-width) — that was once cited as a reason and is not one.

// sortKey is the table's sort axis. sortCanonical is the board's own
// lane-then-priority order — the absence of a field sort, which is why it has
// no direction. The values are the `o` cycle order from t-qve3.
type sortKey int

const (
	// sortNone is deliberately the ZERO value: a column added to tableGeom
	// without an explicit sort stays inert instead of silently becoming a
	// clickable reset-to-canonical target (independent review, finding 7).
	// The price is that Model must initialise tableSort to sortCanonical.
	sortNone sortKey = iota

	sortCanonical
	sortUpdated
	sortCreated
	sortValue
	sortEffort
	sortDue

	sortKeyEnd // one past the last real key, for the cycle
)

func (k sortKey) String() string {
	if k <= sortNone || k >= sortKeyEnd {
		return "?"
	}
	return [...]string{"?", "canonical", "updated", "created", "value", "effort", "due"}[k]
}

// naturalAsc is the direction a key starts in: the order you actually want
// first. Deadlines and cost read smallest-first; recency and worth read
// largest-first.
func (k sortKey) naturalAsc() bool {
	return k == sortEffort || k == sortDue
}

func sortArrow(asc bool) string {
	if asc {
		return glyphSortAsc
	}
	return glyphSortDesc
}

// sortPresent reports whether the task has the field at all. Absent values
// (no due, unset estimates) sort LAST in both directions — flipping a due
// sort must never bury every dated task under the undated majority.
func sortPresent(t *board.Task, k sortKey) bool {
	switch k {
	case sortValue:
		return t.Value > 0
	case sortEffort:
		return t.Effort > 0
	case sortDue:
		return !t.Due.IsZero()
	}
	return true
}

// sortCmp orders two tasks that both HAVE the field, ascending.
func sortCmp(a, b *board.Task, k sortKey) int {
	switch k {
	case sortUpdated:
		return a.Updated.Compare(b.Updated)
	case sortCreated:
		return a.Created.Compare(b.Created)
	case sortValue:
		return a.Value - b.Value
	case sortEffort:
		return a.Effort - b.Effort
	case sortDue:
		return a.Due.Compare(b.Due)
	}
	return 0
}

// tableRows is the filtered board flattened in lane-then-priority order, then
// re-ordered by the active sort. The sort is a LOCAL sort.SliceStable over the
// same snapshot the board renders — furrow `ls --sort` has 4 keys, none of
// them due, and would be a second read racing this one. Stability means ties
// (and every unset value) keep canonical order.
func (m *Model) tableRows() []*board.Task {
	var out []*board.Task
	for _, l := range m.b.Lanes() {
		out = append(out, m.cols[l.Name]...)
	}
	if m.tableSort <= sortCanonical {
		return out
	}
	k, asc := m.tableSort, m.tableSortAsc
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if pa, pb := sortPresent(a, k), sortPresent(b, k); pa != pb {
			return pa
		} else if !pa {
			return false
		}
		c := sortCmp(a, b, k)
		if asc {
			return c < 0
		}
		return c > 0
	})
	return out
}

// setSort applies a sort axis and keeps the SELECTION on the same task — a
// sort that teleports the cursor to whatever now occupies row N is a
// re-ordering of the user, not the rows.
func (m *Model) setSort(k sortKey, asc bool) {
	var keep string
	if t := m.curTask(); t != nil {
		keep = t.ID
	}
	m.tableSort, m.tableSortAsc = k, asc
	if keep != "" {
		m.selectID(keep, false)
	}
	// No note. The filter bar carries the sort for as long as it is not
	// canonical, and the column ▲▼ appear and vanish with it, so both states
	// are already on screen — in a place that survives the next keystroke,
	// which the status line does not.
}

// cycleSort is the `o` key: canonical → updated → created → value → effort →
// due → canonical, entering each key in its natural direction; pressing `o`
// again on a key flips it before moving on. One key covers every state, so a
// mouse-less terminal loses nothing.
func (m *Model) cycleSort() {
	switch {
	case m.tableSort <= sortCanonical:
		m.setSort(sortUpdated, sortUpdated.naturalAsc())
	case m.tableSortAsc == m.tableSort.naturalAsc():
		m.setSort(m.tableSort, !m.tableSortAsc)
	case m.tableSort+1 == sortKeyEnd:
		m.setSort(sortCanonical, false)
	default:
		m.setSort(m.tableSort+1, (m.tableSort + 1).naturalAsc())
	}
}

// tableCol is one column of the flat view: its header, its width, its x
// offset from the slice inset, and the sort axis a header click selects
// (sortNone = not clickable).
type tableCol struct {
	name string
	w, x int
	sort sortKey
}

// Column order. tableGeom returns its slice in exactly this order, so a cell
// renderer indexes by name-as-constant instead of scanning for a header
// string two columns share ("").
const (
	tcCur = iota
	tcID
	tcLane
	tcFlag
	tcVE
	tcTitle
	tcRepo
	tcEpic
	tcLabels
	tcDue
	tcUpdated
	tcDeps
)

// tableGeom measures the table's columns. The renderer and the header
// hit-test both consume THIS slice — the layout.go rule (pixels and geometry
// from one measurement) applied to the flat view, so a header click can never
// land on a boundary the renderer did not draw.
func (m *Model) tableGeom() []tableCol {
	avail := m.w - m.sliceInset()
	cols := []tableCol{
		tcCur:     {name: "", w: 1, sort: sortNone},
		tcID:      {name: "id", w: 8, sort: sortNone},
		tcLane:    {name: "lane", w: 12, sort: sortCanonical}, // canonical IS lane order
		tcFlag:    {name: "", w: 4, sort: sortNone},
		tcVE:      {name: "v/e", w: 6, sort: sortValue}, // 6, not 4: "v/e ▼" must fit
		tcTitle:   {name: "title", sort: sortNone},      // takes the remainder
		tcRepo:    {name: "repo", w: 10, sort: sortNone},
		tcEpic:    {name: "epic", w: 14, sort: sortNone},
		tcLabels:  {name: "labels", w: 12, sort: sortNone},
		tcDue:     {name: "due", w: 10, sort: sortDue},
		tcUpdated: {name: "updated", w: 10, sort: sortUpdated},
		tcDeps:    {name: "deps", w: 5, sort: sortNone},
	}
	fixed := 0
	for _, c := range cols {
		fixed += c.w
	}
	x := 0
	for i := range cols {
		if i == tcTitle {
			cols[i].w = maxInt(16, avail-fixed-len(cols)) // len(cols): the join gaps + 1 spare
		}
		cols[i].x = x
		x += cols[i].w + 1
	}
	return cols
}

// tableClick is the table view's mouse-down surface: a click on a sortable
// header cell sorts by it, a second click flips it — GitHub's table gesture.
// Everything below the header row stays keyboard territory.
func (m *Model) tableClick(x, y int) tea.Cmd {
	if m.mode != modeNormal || y != rowColHdr || m.inPeek(x, y) {
		return nil
	}
	x -= m.sliceInset()
	for _, c := range m.tableGeom() {
		if c.sort == sortNone || x < c.x || x >= c.x+c.w {
			continue
		}
		switch c.sort {
		case sortCanonical:
			m.setSort(sortCanonical, false)
		case m.tableSort:
			m.setSort(c.sort, !m.tableSortAsc)
		default:
			m.setSort(c.sort, c.sort.naturalAsc())
		}
		return nil
	}
	return nil
}

func (m *Model) renderTable() string {
	th := m.th
	rows := m.tableRows()

	// The slice panel insets the rows, so the row budget is what remains —
	// padding to m.w and then shifting right pushed the rightmost cells
	// (due/updated/deps) off the frame edge, where fitFrame truncated them.
	avail := m.w - m.sliceInset()
	cols := m.tableGeom()

	headCells := make([]string, len(cols))
	for i, c := range cols {
		name := c.name
		// The ▲▼ marks the sorted COLUMN, so only keys with a column get one
		// (created and effort have none); the filter bar names the sort for
		// all of them — see chromeLayers.
		if c.sort == m.tableSort && m.tableSort > sortCanonical {
			name += " " + sortArrow(m.tableSortAsc)
		}
		headCells[i] = pad(name, c.w)
	}
	head := strings.Join(headCells, " ")

	var body []string
	for i, t := range rows {
		glyph, styleFor := cardMarker(t, m.g)
		deps := ""
		if n := len(t.Deps); n > 0 {
			if b := len(m.g.BlockedBy(t.ID)); b > 0 {
				deps = th.danger.Render(fmt.Sprintf("%d/%d", b, n))
			} else {
				deps = th.ok.Render(fmt.Sprintf("0/%d", n))
			}
		}
		ve := ""
		if t.Value > 0 || t.Effort > 0 {
			ve = fmt.Sprintf("%d/%d", t.Value, t.Effort)
		}
		epic := t.Epic
		if e := m.b.Epic(t.Epic); e != nil {
			epic = e.Title
		}
		due, overdue := "", false
		if !t.Due.IsZero() {
			// Local, not UTC: an evening-local due formatted in UTC reads one
			// day early (the peek learnt this the hard way — t-qve3 rides it).
			due = t.Due.Local().Format("2006-01-02")
			overdue = t.Due.Before(nowFn()) && t.Closed.IsZero()
		}
		cur := " "
		if i == m.tableIdx {
			// A glyph, not just colour: `-dump -plain` has to show the cursor.
			cur = "▌"
		}
		plain := make([]string, len(cols))
		plain[tcCur] = cur
		plain[tcID] = pad(t.ID, cols[tcID].w)
		plain[tcLane] = pad(t.Status, cols[tcLane].w)
		plain[tcFlag] = pad(glyph, cols[tcFlag].w)
		plain[tcVE] = pad(ve, cols[tcVE].w)
		plain[tcTitle] = pad(t.Title, cols[tcTitle].w)
		plain[tcRepo] = pad(t.ShortRepo(), cols[tcRepo].w)
		plain[tcEpic] = pad(epic, cols[tcEpic].w)
		plain[tcLabels] = pad(strings.Join(t.Labels, ","), cols[tcLabels].w)
		plain[tcDue] = pad(due, cols[tcDue].w)
		plain[tcUpdated] = pad(ago(t.Updated), cols[tcUpdated].w)
		plain[tcDeps] = pad(ansiStrip(deps), cols[tcDeps].w)
		var line string
		if i == m.tableIdx {
			// One inverse band rather than a patchwork of per-cell colours.
			line = th.invert.Render(pad(strings.Join(plain, " "), avail))
		} else {
			dueStyle := th.muted
			if overdue {
				// The same meaning as `is:overdue`: a promise in the past on a
				// task that is still open.
				dueStyle = th.danger
			}
			styled := make([]string, len(cols))
			copy(styled, plain)
			styled[tcID] = th.dim.Render(plain[tcID])
			styled[tcFlag] = styleFor(th).Render(plain[tcFlag])
			styled[tcRepo] = th.chipAlt.Render(plain[tcRepo])
			styled[tcEpic] = th.muted.Render(plain[tcEpic])
			styled[tcLabels] = th.muted.Render(plain[tcLabels])
			styled[tcDue] = dueStyle.Render(plain[tcDue])
			styled[tcUpdated] = th.dim.Render(plain[tcUpdated])
			styled[tcDeps] = pad(deps, cols[tcDeps].w)
			line = strings.Join(styled, " ")
		}
		body = append(body, pad(line, avail))
	}

	layers := []*lg.Layer{
		lg.NewLayer(blankCanvas(m.w, m.h)).X(0).Y(0).Z(zChrome - 1),
	}
	layers = append(layers, m.chromeLayers()...)
	// The slice panel insets the table the way it insets the board columns —
	// rows shift right instead of hiding their id column under the panel.
	inset := m.sliceInset()
	layers = append(layers,
		lg.NewLayer(th.colHdr.Render(pad(head, avail))).X(inset).Y(rowColHdr).Z(zChrome),
		// maxInt, not m.w: strings.Repeat panics on a negative count, and a
		// negative width reached it (via `-dump -w -1`, before the geometry
		// flags became -cols/-rows; TestAdvTableViewPanicsOnNegativeWidth
		// pins it directly now).
		lg.NewLayer(th.rule.Render(strings.Repeat("─", maxInt(m.w, 1)))).X(0).Y(rowColSum).Z(zChrome),
	)

	const tableTop = rowRule // the table needs no per-column rule row
	visRows := m.h - tableTop - footerH
	top := clamp(m.tableIdx-visRows/2, 0, maxInt(0, len(body)-visRows))
	for i := top; i < len(body) && tableTop+i-top < m.h-footerH; i++ {
		layers = append(layers, lg.NewLayer(body[i]).X(inset).Y(tableTop+i-top).Z(zCard))
	}
	if m.peekOpen {
		layers = append(layers, m.peekLayer())
	}
	if m.sliceOpen {
		// The slice narrows the table rows exactly as it narrows the board
		// columns, and `s` reaches this view too — an invisible panel that
		// still owned the keyboard ate every arrow key (reviewed live).
		layers = append(layers, m.sliceLayer())
	}
	if m.mode == modeEdit {
		if l := m.editLayer(); l != nil {
			layers = append(layers, l)
		}
	}
	if m.mode == modeAdd {
		layers = append(layers, m.addLayer())
	}
	if m.fullHelp {
		layers = append(layers, m.helpLayer())
	}
	return m.fitFrame(lg.NewCompositor(layers...).Render())
}

// ansiStrip removes escape sequences. Used for the inverse-highlighted table
// row and for `-dump -plain`, whose whole point is a diffable frame.
//
// It handles the WHOLE CSI/OSC grammar, not just SGR. The narrower version it
// replaces ended a sequence only on 'm' or 'K', which is fine for lipgloss
// output (SGR only) and silently corrupting on anything else: given
// "\x1b[6;31Hmoved" it kept hunting for a terminator, found the 'm' of the WORD
// "moved", and ate the live text in between. A stripper that deletes real
// content mid-word is a trap for the next person who points it at a captured
// terminal stream.
func ansiStrip(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] != 0x1b {
			b.WriteByte(s[i])
			i++
			continue
		}
		i++
		if i >= len(s) {
			break
		}
		switch s[i] {
		case '[': // CSI: parameter/intermediate bytes, then a final byte in @..~
			i++
			for i < len(s) && (s[i] < 0x40 || s[i] > 0x7e) {
				i++
			}
			i++
		case ']': // OSC: runs to BEL or ST
			i++
			for i < len(s) {
				if s[i] == 0x07 {
					i++
					break
				}
				if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
					i += 2
					break
				}
				i++
			}
		default: // a two-byte escape
			i++
		}
	}
	return b.String()
}

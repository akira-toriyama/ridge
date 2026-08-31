package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

// z-order. The compositor sorts on these, which is the whole reason this POC
// uses lipgloss v2's Layer/Compositor rather than string concatenation: a
// side-peek, a drop indicator and a dragged ghost are three different planes
// over the same board.
const (
	zChrome = 0
	zColBG  = 9 // the column container, under every card
	zCard   = 10
	zDrop   = 50 // drop indicator — deliberately given NO id so Hit() skips it
	zPeek   = 80
	zEdit   = 85 // the field-edit overlay, above the peek, below `?`
	zHelp   = 90
	zGhost  = 99 // the dragged card, above everything
)

// View renders one frame. Everything that used to be a NewProgram option in
// bubbletea v1 is now a field re-asserted here every render — which is exactly
// what makes runtime mouse toggling (the `M` key) free.
func (m *Model) View() tea.View {
	m.relayout()
	var content string
	switch m.view {
	case viewTable:
		content = m.renderTable()
	case viewGraph:
		content = m.renderGraph()
	case viewMap:
		content = m.renderMap()
	case viewBoxes:
		content = m.renderBoxes()
	case viewRoadmap:
		content = m.renderRoadmap()
	default:
		content = m.renderBoard()
	}
	v := tea.NewView(content)
	v.AltScreen = true
	v.MouseMode = tea.MouseModeNone
	if m.mouseOn {
		// CellMotion (1002), never AllMotion (1003): 1002 still delivers motion
		// events while a button is held — which is all a drag needs — and has
		// far better tmux/mosh support.
		v.MouseMode = tea.MouseModeCellMotion
	}
	// Ask for the Kitty keyboard protocol. Without it a terminal CANNOT ENCODE A
	// MODIFIED SPACE at all: shift+space arrives as a bare space, so the graph
	// binding is simply unreachable and the peek opens instead. That is not a
	// theory — it is what the first person to run this POC hit. Ghostty, kitty,
	// WezTerm and recent iTerm2 honour this; everywhere else the "S" alias is the
	// way in, which is why every gesture here keeps a plain-key twin.
	// ReportAllKeysAsEscapeCodes is the one that matters: a plain space is sent
	// as TEXT, and text carries no modifier, so shift+space is indistinguishable
	// from space until every key comes back as an escape code. Basic
	// disambiguation (flag 1, always on) is not enough for this gesture.
	v.KeyboardEnhancements = tea.KeyboardEnhancements{ReportAllKeysAsEscapeCodes: true}
	v.WindowTitle = "furrow board"
	return v
}

// fitFrame is the hard backstop: no frame may ever be larger than the terminal
// it was handed. Individual pieces clamp themselves, but a compositor grows its
// canvas to fit any layer, so one oversized child (a card wider than a 20-column
// terminal, a peek taller than 5 rows) silently expanded the whole frame — which
// is how a 1x1 terminal rendered 28x6.
func (m *Model) fitFrame(s string) string {
	return lg.NewStyle().MaxWidth(maxInt(m.w, 1)).MaxHeight(maxInt(m.h, 1)).Render(s)
}

func blankCanvas(w, h int) string {
	row := strings.Repeat(" ", maxInt(w, 1))
	rows := make([]string, maxInt(h, 1))
	for i := range rows {
		rows[i] = row
	}
	return strings.Join(rows, "\n")
}

func (m *Model) renderBoard() string {
	layers := []*lg.Layer{lg.NewLayer(blankCanvas(m.w, m.h)).X(0).Y(0).Z(zChrome - 1)}
	layers = append(layers, m.chromeLayers()...)
	layers = append(layers, m.columnLayers()...)
	if m.sliceOpen {
		layers = append(layers, m.sliceLayer())
	}
	if m.peekOpen {
		layers = append(layers, m.peekLayer())
	}
	layers = append(layers, m.modalLayers()...)
	if g := m.ghostLayer(); g != nil {
		layers = append(layers, g)
	}
	return m.fitFrame(lg.NewCompositor(layers...).Render())
}

// modalLayers is every overlay that OWNS THE KEYBOARD, listed ONCE. The board
// and the table each compose their own frame, and this list used to be written
// out in both — so an overlay added to one of them held the keyboard while
// rendering nowhere at all in the other. That is the same failure the slice
// panel's own comment in table.go records ("an invisible panel that still owned
// the keyboard ate every arrow key"). A new mode belongs here, never in a caller.
//
// The help overlay is last because it sits above all of them (zHelp).
func (m *Model) modalLayers() []*lg.Layer {
	var out []*lg.Layer
	switch m.mode {
	case modeEdit:
		if l := m.editLayer(); l != nil {
			out = append(out, l)
		}
	case modeAdd:
		out = append(out, m.addLayer())
	case modeEpic:
		if l := m.epicLayer(); l != nil {
			out = append(out, l)
		}
	}
	if m.fullHelp {
		out = append(out, m.helpLayer())
	}
	return out
}

// fullTabs is the tab strip the full-screen views carry in their title bars:
// every view, the current one lit. Spelled ONCE for DemoNames' reason — the
// strip was hand-written per view and drifted: the graph's and the map's
// never learned the box overview existed (three copies; PR #60 grew one).
// The board's own chrome keeps its two-tab strip on purpose: Board|Table is
// its interactive toggle (`v`), not a listing of everywhere a key could go.
func (m *Model) fullTabs(active viewKind) string {
	th := m.th
	parts := make([]string, 0, 6)
	for _, tab := range []struct {
		v    viewKind
		name string
	}{
		{viewBoard, "Board"}, {viewTable, "Table"}, {viewGraph, "Graph"},
		{viewMap, "Map"}, {viewBoxes, "Boxes"}, {viewRoadmap, "Roadmap"},
	} {
		if tab.v == active {
			parts = append(parts, th.tabOn.Render(tab.name))
		} else {
			parts = append(parts, th.tabOff.Render(tab.name))
		}
	}
	return strings.Join(parts, th.dim.Render(" │ "))
}

// minFilterInputW is the floor under the filter input when the chips squeeze
// it. Below this the input cannot show what is being typed; on the declared
// 240-column floor the chips never get close to forcing it.
const minFilterInputW = 20

// chromeLayers draws the title bar, the filter bar and the status line.
func (m *Model) chromeLayers() []*lg.Layer {
	th := m.th
	total := len(m.b.Tasks())
	shown := m.countVisible()

	// The Board | Table tab strip. `v` toggled the table view with no on-screen
	// affordance at all; the tab row under the project title is GitHub's most
	// recognisable chrome after the columns themselves.
	tab := func(name string, on bool) string {
		if on {
			return th.tabOn.Render(name)
		}
		return th.tabOff.Render(name)
	}
	tabs := tab("Board", m.view == viewBoard) + th.dim.Render(" │ ") + tab("Table", m.view == viewTable)

	// The saved-view tabs extend the strip (t-es5v). They sit AFTER the
	// layout pair because they are the bigger object: a view tab carries a
	// whole {layout, q, sort, slice} bundle, of which Board|Table names one
	// dimension of the current state.
	left := th.title.Render("furrow board") + th.crumb.Render("  ·  ") + tabs + m.viewTabStrip()
	counts := fmt.Sprintf("%d/%d tasks", shown, total)
	if shown == total {
		counts = fmt.Sprintf("%d tasks", total)
	}
	// The passive latency readout: the last persist and how long the store
	// took. Empty until the first real write, so fixture frames are unchanged.
	// `? help` rides up here rather than in a footer because it never changes:
	// the top rows carry standing state, the bottom row carries the one
	// message that is about to be replaced. It is also the whole in-app
	// pointer to the key surface now that the footers are gone.
	tail := counts
	if m.lastPersist != "" {
		tail += "  ·  " + m.lastPersist
	}
	tail += "  ·  " + m.modeBadge()
	// The mouse toggle used to share the badge slot with the modes; it is
	// standing state of its own kind, so it gets its own token.
	if !m.mouseOn {
		tail += th.dim.Render("  ·  mouse off")
	}
	tail += th.dim.Render("  ·  ? help")
	right := th.crumb.Render(tail)
	title := joinEnds(left, right, m.w)

	// The chips the input's own padding must not evict: the slice is part of
	// what the board is filtered by, so it shows even while the panel is
	// closed — state the panel set must never be invisible state. The table's
	// sort, by the same rule: the header ▲▼ only exists for keys that HAVE a
	// column — created and effort do not — and the status line is overwritten
	// by the next keystroke, so this is the one place the sort stays readable
	// Built BEFORE the input so the input
	// can be sized to the space they leave. The qErr/pinned right-aligners
	// below still outrank them — joinEnds truncates from the left, and the
	// padded input reaches the chips first; that trade predates this sizing
	// (identical on a fixed-width input) and an error beats a chip.
	chips := ""
	if t := m.sliceTerm(); t != "" {
		chips += th.dim.Render("  slice ") + th.accent.Render(t)
	}
	if m.view == viewTable && m.tableSort > sortCanonical {
		chips += th.dim.Render("  sort ") +
			th.accent.Render(m.tableSort.String()+" "+sortArrow(m.tableSortAsc))
	}
	var filter string
	switch {
	case m.mode == modeFilter:
		// The input pads to its whole width, so its width IS the layout: a
		// fixed number here (w-30 for years) overran the row the moment the
		// chips outgrew the remainder, and the sort readout fell off exactly
		// while the user was typing a filter (t-a54p). Size it to the frame
		// being drawn instead — render-time state derived from render-time
		// measurement, the same rule the card measurer lives by.
		avail := maxInt(minFilterInputW, m.w-lg.Width(chips))
		m.ti.SetWidth(avail)
		// SetWidth alone does not re-run the input's overflow bookkeeping
		// (bubbles v2 recomputes the horizontal window only on cursor moves),
		// so a value seeded at another width keeps that width's scroll window
		// — entering the mode with an existing query showed only its tail in
		// a 48-cell slice of a 237-cell input. SetCursor to the position the
		// cursor is already at is the documented no-op that forces the recompute.
		m.ti.SetCursor(m.ti.Position())
		filter = m.ti.View()
		if over := lg.Width(filter) + lg.Width(chips) - m.w; over > 0 {
			// SetWidth budgets the text area; the prompt and cursor cell ride
			// on top of it. Measure the real render once and give the
			// overhead back rather than hard-coding bubbles' chrome width.
			m.ti.SetWidth(maxInt(minFilterInputW, avail-over))
			m.ti.SetCursor(m.ti.Position())
			filter = m.ti.View()
		}
	case m.qRaw == "":
		// GitHub's literal placeholder. The old text spelled out a full example
		// query, which read as an ACTIVE filter — in every dump the bar said
		// "lane:ready repo:vista is:blocked" while the header said "24 tasks"
		// and every lane was full. The syntax belongs in the ? overlay.
		filter = th.dim.Render("/ Filter by keyword or by field")
	default:
		filter = th.dim.Render("/ ") + th.chipAlt.Render(m.qRaw)
	}
	filter += chips
	if m.qErr != "" {
		filter = joinEnds(filter, th.errText.Render("⚠ "+m.qErr), m.w)
	}
	if len(m.pinned) > 0 {
		filter = joinEnds(filter, th.accent.Render(fmt.Sprintf("+%d pinned by jump", len(m.pinned))), m.w)
	}

	// The last row is the status line and nothing else. It stays on the canvas
	// even when empty so that a message appearing never shifts the board.
	//
	// maxInt(0, …): on a 1-row terminal m.h-1 is 0 and anything negative would
	// be NORMALISED by the compositor, which shifts the whole scene down — a
	// 1-row terminal once came back 6 rows tall. Clamp and let fitFrame trim.
	return []*lg.Layer{
		lg.NewLayer(pad(title, m.w)).X(0).Y(rowTitle).Z(zChrome),
		lg.NewLayer(pad(filter, m.w)).X(0).Y(rowFilter).Z(zChrome),
		lg.NewLayer(pad(m.statusLine(), m.w)).X(0).Y(maxInt(0, m.h-footerH)).Z(zChrome),
	}
}

// modeBadge names the current mode, ALWAYS — normal included. The modes are
// invisible state otherwise: nothing else on screen says whose keyboard this
// is, and the `?` overlay's sections are keyed to these same names (glossary:
// mode). ⟨DRAG⟩ is the one non-mode badge — a drag lives in dragState, not in
// m.mode, but it owns the gesture just as completely.
func (m *Model) modeBadge() string {
	th := m.th
	if m.drag.moved {
		return th.accent.Render("⟨DRAG⟩")
	}
	switch m.mode {
	case modeMove:
		return th.accent.Render("⟨MOVE⟩")
	case modeFilter:
		return th.chipAlt.Render("⟨FILTER⟩")
	case modeEdit:
		return th.chipAlt.Render("⟨EDIT⟩")
	case modeAdd:
		return th.chipAlt.Render("⟨ADD⟩")
	case modeSlice:
		return th.chipAlt.Render("⟨SLICE⟩")
	case modeEpic:
		return th.chipAlt.Render("⟨EPIC⟩")
	}
	return th.dim.Render("⟨NORMAL⟩")
}

func (m *Model) statusLine() string {
	th := m.th
	// A gesture owns this row, but this row is also m.status's ONLY render
	// site. Returning the gesture string alone meant a persist refusal landing
	// while a card was lifted rendered nowhere at all, and the rollback that
	// followed was completely silent. Append instead of replace: the [slot N]
	// readout has to stay while the card is still in the air.
	warn := ""
	if m.statusErr {
		warn = th.errText.Render("   ⚠ " + m.status)
	}
	if m.mode == modeMove {
		return th.accent.Render(fmt.Sprintf("%s MOVE %s → %s [slot %d]   ⏎ commit · esc restore",
			glyphLift, m.moveID, m.dropLane, m.dropIdx)) + warn
	}
	if m.drag.moved {
		// Same predicate as the release and the drop indicator, and the same
		// lane it resolves — naming the motion-time cache here could announce
		// a different column from the one the bar marks. Saying "release to
		// drop" over the chrome, the gutter or the peek was a lie the release
		// then made good on by cancelling.
		to, ok := m.dropTarget(m.drag.x, m.drag.y)
		if !ok {
			return th.accent.Render(fmt.Sprintf(
				"%s DRAG %s   off the board — release cancels · drag back over a column to drop",
				glyphLift, m.drag.id)) + warn
		}
		return th.accent.Render(fmt.Sprintf("%s DRAG %s → %s [slot %d]   release to drop · esc cancel",
			glyphLift, m.drag.id, to, m.lay.idxAtY(to, m.drag.y))) + warn
	}
	if m.statusErr {
		return th.errText.Render("⚠ " + m.status)
	}
	return th.status.Render(m.status)
}

// columnLayers draws every visible column: container, header, rule, cards, drop
// indicator.
func (m *Model) columnLayers() []*lg.Layer {
	th := m.th
	var out []*lg.Layer
	for i := range m.lay.Cols {
		c := &m.lay.Cols[i]
		focused := c.Lane.Name == m.curLaneName()

		// The column CONTAINER. Without an inset background an empty column is
		// literally invisible — at 200 columns the right third of the screen
		// read as broken rather than as "these lanes are empty" — and an
		// invisible column is not an obvious drop target either. No ID(), so
		// Compositor.Hit() skips it and card hit-testing is untouched.
		if bh := c.Bot - c.Top; bh > 0 {
			bg := th.colBG
			if focused {
				bg = th.colBGOn
			}
			out = append(out, lg.NewLayer(bg.Render(blankCanvas(c.W, bh))).
				X(c.X).Y(c.Top).Z(zColBG))
		}

		hdrStyle := th.colHdr
		ruleStyle := th.rule
		if focused {
			hdrStyle, ruleStyle = th.colHdrOn, th.chipAlt
		}
		// Lane dot + display name + count, all packed at the LEFT. The count used
		// to be flush right, 22 cells of dead space away from the name, which is
		// spreadsheet grammar and fights the "board" read.
		count := fmt.Sprintf("%d", len(c.Tasks))
		if c.Lane.WIP > 0 {
			// GitHub Projects parity: the limit is RENDERED, never enforced.
			count = fmt.Sprintf("%d/%d", len(c.Tasks), c.Lane.WIP)
			if len(c.Tasks) > c.Lane.WIP {
				count = th.warn.Render(count + glyphWIPOver)
			} else {
				count = th.colCount.Render(" " + count + " ")
			}
		} else {
			count = th.colCount.Render(" " + count + " ")
		}
		hdr := th.laneDot(c.Lane).Render(glyphLaneDot) + " " +
			hdrStyle.Render(c.Lane.DisplayName()) + " " + count
		out = append(out, lg.NewLayer(pad(hdr, c.W)).X(c.X).Y(rowColHdr).Z(zChrome))

		sv, se := 0, 0
		for _, t := range c.Tasks {
			sv += t.Value
			se += t.Effort
		}
		// A number-field sum of zero under an empty lane is noise in the densest
		// part of the chrome; GitHub only shows sums it has something to sum.
		sum := ""
		if sv+se > 0 {
			sum = th.dim.Render(fmt.Sprintf("v%d e%d", sv, se))
		}
		var hint string
		switch {
		case c.Hidden > 0:
			hint = th.dim.Render(fmt.Sprintf("+%d below", c.Hidden))
		case c.Scroll > 0:
			hint = th.dim.Render(fmt.Sprintf("%d above", c.Scroll))
		}
		out = append(out, lg.NewLayer(joinEnds(sum, hint, c.W)).X(c.X).Y(rowColSum).Z(zChrome))
		out = append(out, lg.NewLayer(ruleStyle.Render(strings.Repeat("─", maxInt(c.W, 1)))).
			X(c.X).Y(rowRule).Z(zChrome))

		for _, box := range c.Cards {
			t := c.Tasks[box.Idx]
			st := cardNormal
			switch {
			case m.drag.moved && t.ID == m.drag.id:
				st = cardShadow
			case m.mode == modeMove && t.ID == m.moveID:
				st = cardLifted
			case focused && box.Idx == m.curPos() && m.mode != modeMove:
				st = cardSelected
			}
			out = append(out, lg.NewLayer(renderCard(t, m.g, th, c.W, st)).
				ID("task:"+t.ID).X(box.X).Y(box.Y).Z(zCard))
		}

		if c.Scroll > 0 && c.Bot > c.Top {
			out = append(out, lg.NewLayer(th.dim.Render("▲")).X(c.X+maxInt(0, c.W-2)).Y(c.Top).Z(zCard+1))
		}
	}
	if l := m.dropLayer(); l != nil {
		out = append(out, l)
	}
	return out
}

// dropLayer draws the insertion marker for whichever gesture is running. It is
// given NO id, so Compositor.Hit() skips it — a drop indicator must never
// swallow the click that is aimed at the card underneath.
//
// It is drawn as a bracketed dashed caret rather than a solid hairline: a solid
// bar was visually the same object as the header rule under each column and read
// as a section divider, not an insertion point.
func (m *Model) dropLayer() *lg.Layer {
	var lane string
	var idx int
	switch {
	case m.mode == modeMove:
		lane, idx = m.dropLane, m.dropIdx
	case m.drag.moved:
		// Ask the same predicate the RELEASE asks, take the lane IT resolves,
		// and ask now rather than trusting values cached at motion time: an
		// auto-scroll relayout or a resize moves the columns under a pointer
		// that never sent another event, so the cached lane can name a column
		// the release would not choose. Off the board there is no slot to mark
		// — the release cancels, and an insertion bar promising otherwise made
		// the "will drop" and "will cancel" frames byte-identical.
		to, ok := m.dropTarget(m.drag.x, m.drag.y)
		if !ok {
			return nil
		}
		lane, idx = to, m.lay.idxAtY(to, m.drag.y)
	default:
		return nil
	}
	c := m.lay.Col(lane)
	if c == nil || c.Bot <= c.Top {
		return nil
	}
	y, ok := m.lay.dropY(lane, idx)
	if !ok {
		return nil
	}
	bar := glyphDropL + strings.Repeat(glyphDrop, maxInt(0, c.W-2)) + glyphDropR
	return lg.NewLayer(m.th.dropInd.Render(pad(bar, c.W))).
		X(c.X).Y(clamp(y, c.Top, maxInt(c.Top, c.Bot-1))).Z(zDrop)
}

// ghostLayer is the dragged card following the cursor, offset by where it was
// grabbed so it does not snap under the pointer.
func (m *Model) ghostLayer() *lg.Layer {
	if !m.drag.moved {
		return nil
	}
	t := m.b.Task(m.drag.id)
	if t == nil {
		return nil
	}
	// The layout's negotiated width, not the preferred constant: on a terminal
	// narrower than one preferred column the ghost was still drawn at 28 cells
	// and the clamp degenerated to 0, so the ghost alone widened the frame.
	cw := minInt(m.lay.ColW, maxInt(m.w, 1))
	card := renderCard(t, m.g, m.th, cw, cardGhost)
	x := clamp(m.drag.x-m.drag.grabDX, 0, maxInt(0, m.w-cw))
	y := clamp(m.drag.y-m.drag.grabDY, 0, maxInt(0, m.h-lg.Height(card)))
	return lg.NewLayer(card).ID("ghost").X(x).Y(y).Z(zGhost)
}

// helpLayer is the `?` overlay. bubbles' FullHelpView lays every group out on
// ONE row and does not consult SetWidth, so at 80 columns it renders a 98-cell
// block and silently overflows the frame; the groups are packed into as many
// rows as the terminal actually has room for.
func (m *Model) helpLayer() *lg.Layer {
	inner := maxInt(16, m.w-6)

	// One titled block per mode section, so a key is read under the mode that
	// answers it. The current mode's heading is lit — the overlay opens from
	// normal mode, move mode, the graph and the dep map, so "which of these
	// blocks is mine right now" is a real question. Mode outranks view:
	// a lift is a lift on whatever screen it started from.
	now := "normal mode"
	switch {
	case m.mode == modeMove:
		now = "move mode"
	case m.view == viewGraph:
		now = "graph"
	case m.view == viewMap:
		now = "dep map"
	case m.view == viewBoxes:
		now = "box overview"
	case m.view == viewRoadmap:
		now = "roadmap"
	}

	// Render each section as its own block first…
	var blocks []string
	for _, sec := range m.keys.HelpSections(m.view == viewTable || m.peekOpen) {
		hdr := m.th.dim.Render(sec.title)
		if sec.title == now {
			hdr = m.th.accent.Render(sec.title + " — you are here")
		}
		rows := []string{hdr}

		var cur [][]key.Binding
		flush := func() {
			if len(cur) > 0 {
				rows = append(rows, m.help.FullHelpView(cur))
				cur = nil
			}
		}
		for _, grp := range sec.groups {
			cand := append(append([][]key.Binding{}, cur...), grp)
			if len(cur) > 0 && lg.Width(m.help.FullHelpView(cand)) > inner {
				flush()
				cur = [][]key.Binding{grp}
				continue
			}
			cur = cand
		}
		flush()
		blocks = append(blocks, strings.Join(rows, "\n"))
	}

	// …then shelve the blocks side by side while they fit. Stacking them
	// vertically cost ~6 rows over the flat overlay and, at 240×24 — a stock
	// terminal height on the repo's stated width floor — the centred box
	// covered the title bar: the frame lost the very mode badge this overlay
	// is sectioned around. Width is the abundant dimension here; spend it.
	const shelfGap = "    "
	gapW := lg.Width(shelfGap)
	var shelves []string
	var shelf []string
	shelfW := 0
	for _, b := range blocks {
		w := lg.Width(b)
		if len(shelf) > 0 && shelfW+gapW+w > inner {
			shelves = append(shelves, lg.JoinHorizontal(lg.Top, shelf...))
			shelf, shelfW = nil, 0
		}
		if len(shelf) > 0 {
			shelf = append(shelf, shelfGap)
			shelfW += gapW
		}
		shelf = append(shelf, b)
		shelfW += w
	}
	shelves = append(shelves, lg.JoinHorizontal(lg.Top, shelf...))
	rows := []string{strings.Join(shelves, "\n\n")}

	syntax := wrapJoin([]string{"filter syntax (furrow -q):",
		"field:value · comma = OR · leading - negates · no:/has:",
		"· is:actionable|blocked|stale|open|closed|draft|unfiled|overdue",
		"· value:>=4 · updated:>=-2w · epic:/depends-on:/blocks: · free words over title+body"}, " ", inner)
	// The 1-9/V bindings above cannot carry the FILE, and the file is the
	// rename/delete surface — this is the one place in the app to learn it.
	viewsLine := wrapJoin([]string{"saved views (1-9/V):",
		"named {layout, q, sort, slice} bundles from ~/.config/ridge/views.toml — V saves, the file renames"}, " ", inner)
	note := wrapJoin([]string{"every mouse gesture above has a keyboard twin —",
		"that is the rule, not a bonus"}, " ", inner)
	box := m.th.peek.Render(m.th.peekHdr.Render("keys") + "\n\n" +
		strings.Join(rows, "\n") + "\n\n" +
		m.th.dim.Render(syntax) + "\n" + m.th.dim.Render(viewsLine) + "\n" + m.th.dim.Render(note))
	// Hard backstop: an overlay must never be able to overflow the frame —
	// and row 0 is not the overlay's to take. The title bar carries the mode
	// badge and the tab strip; when the terminal is too short for both, the
	// overlay is the one that gives (its bottom is trimmed), because a help
	// screen that hides which mode you are in defeats its own sections.
	box = lg.NewStyle().MaxWidth(m.w).MaxHeight(maxInt(1, m.h-1)).Render(box)

	x := maxInt(0, (m.w-lg.Width(box))/2)
	y := maxInt(1, (m.h-lg.Height(box))/2)
	return lg.NewLayer(box).ID("help").X(x).Y(y).Z(zHelp)
}

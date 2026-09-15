package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

// What every full-screen view shares: the frame skeleton and title line
// above, and below it the cursor-pinned scroll, the half-page key, the
// filter's status-line claim, the four keys every view answers alike, and the
// cursor carried back to the board on close. Each view keeps its own layout,
// keys and words; these hold the invariants once, so the copies cannot drift
// apart (a roster of the views used to sit here, and it was one view short
// within a month). There is no fullScreenView interface on purpose: the
// layouts share no type, and inventing one would be abstraction for its own
// sake.
//
// Not everything here serves all six. packBands and ruleHead are the two
// PACKED overviews' (the dep map's and the box overview's), and windowBands
// serves the four views that materialise every band; each says so itself.
// They live here because this is where the frame is built, not because the
// roster grew. The graph's scroll follows a two-axis frame of its own and is not a
// scrollToSel client.

// fullScreenTitleBar is the title line a full-screen view draws in place of
// the board's: the shared prefix and tab strip on the left; the view's counts,
// its ⟨TOKEN⟩ and the `? help` pointer on the right. That pointer is the only
// way to the key surface from inside a full-screen mode, since none has a
// footer. The roadmap builds its own left half (the saved-view tab strip rides
// there) from the two halves below.
func (m *Model) fullScreenTitleBar(v viewKind, counts, token string) string {
	return joinEnds(m.fullScreenTitleLeft(v), m.fullScreenTitleRight(counts, token), m.w)
}

func (m *Model) fullScreenTitleLeft(v viewKind) string {
	return m.th.title.Render("furrow board") + m.th.crumb.Render("  ·  ") + m.fullTabs(v)
}

func (m *Model) fullScreenTitleRight(counts, token string) string {
	return m.th.crumb.Render(counts+"  ·  ") + m.th.accent.Render(token) + m.th.dim.Render("  ·  ? help")
}

// fillCanvas gives every rendered line the one-cell left margin and the frame
// width, and blank-fills to h lines so the strip and status line always land
// on the same rows.
func (m *Model) fillCanvas(lines []string, h int) []string {
	out := make([]string, 0, h)
	for _, s := range lines {
		out = append(out, " "+pad(s, maxInt(1, m.w-2)))
	}
	for len(out) < h {
		out = append(out, strings.Repeat(" ", maxInt(1, m.w)))
	}
	return out
}

// placedBlock is one rendered block and the packed cell it starts at. Lines
// are already composed to the column's width by the block renderer.
type placedBlock struct {
	Col, Y int
	Lines  []string
}

// packBands lays placed blocks into a cols x h grid and returns one string per
// screen row. Every column contributes exactly colW cells at every row, so the
// join is width-exact and the columns cannot drift apart as the rows below
// them get longer. gap stays a parameter: the two overviews' gaps are separate
// constants that happen to be equal, and hard-coding one here would retune the
// other view's geometry the next time either is tuned.
//
// Col is indexed UNGUARDED on purpose. packColumns answers ncols as the last
// column it actually used, so an out-of-range Col is a packing bug; a bounds
// guard here would turn that panic into a block silently dropped off the
// frame. The Y clip is the same shape in reverse and equally unreachable —
// both block renderers return exactly the height the packer measured.
func packBands(blocks []placedBlock, cols, colW, h, gap int) []string {
	blank := strings.Repeat(" ", colW)
	grid := make([][]string, cols)
	for c := range grid {
		grid[c] = make([]string, h)
		for y := range grid[c] {
			grid[c][y] = blank
		}
	}
	for _, b := range blocks {
		for j, line := range b.Lines {
			if y := b.Y + j; y < h {
				grid[b.Col][y] = line
			}
		}
	}

	sep := strings.Repeat(" ", gap)
	bands := make([]string, h)
	row := make([]string, cols)
	for y := 0; y < h; y++ {
		for c := range grid {
			row[c] = grid[c][y]
		}
		// Right-trimmed, and re-padded later by fillCanvas: the trailing blank
		// of the last column is the one part of a band no frame needs.
		bands[y] = strings.TrimRight(strings.Join(row, sep), " ")
	}
	return bands
}

// ruleHead is the rule line naming a packed block: two rule cells, the label,
// then rule out to the column's width. Only the two packed overviews draw it.
// The sweep's section header looks like this and is not one — it carries a
// third styled segment and ends in ansi.Truncate rather than pad, so folding
// it in would give it both trailing padding and an ellipsis it does not have
// — and peek.go's sectionRule is the peek's own shape.
func (m *Model) ruleHead(label string, w int) string {
	head := m.th.rule.Render("──") + m.th.peekHdr.Render(label)
	if n := w - lg.Width(head); n > 0 {
		head += m.th.rule.Render(strings.Repeat("─", n))
	}
	return pad(head, w)
}

// composeFullScreen is the frame every full-screen view ends in: title bar,
// header, canvas, the task strip when the window has room for one (strip
// renders it at that height), the status line, clipped to the window — and
// whatever owns the keyboard layered on top. The frame is a string, not a
// compositor scene, so those layers must be added HERE: the graph once let `?`
// set fullHelp and change not one pixel (the next Esc went on clearing an
// invisible flag — harmless while the graph had a footer of its own, a lie
// once its title bar advertised `? help`), and the box overview — the one
// view that opens a modal from inside itself (`m`) — once handed the keyboard
// to the epic overlay while rendering none of it. modalLayers is the one home
// for "what owns the keyboard" (the edit / add / epic overlays, then help), so
// every view routes through it: a view need not open a modal to be inside one
// — `-roadmap` composes with every `-demo`, so the roadmap can START under the
// add or edit overlay, and until this funnel those frames drew no modal at all.
func (m *Model) composeFullScreen(titleBar, header string, canvas []string, strip func(h int) string) string {
	parts := []string{pad(titleBar, m.w), pad(header, m.w), strings.Join(canvas, "\n")}
	if sh := m.stripHeight(); sh > 0 {
		parts = append(parts, strip(sh))
	}
	parts = append(parts, pad(m.statusLine(), m.w))
	frame := m.fitFrame(strings.Join(parts, "\n"))
	if layers := m.modalLayers(); len(layers) > 0 {
		frame = m.fitFrame(lg.NewCompositor(append(
			[]*lg.Layer{lg.NewLayer(frame).X(0).Y(0).Z(zChrome)}, layers...)...).Render())
	}
	return frame
}

// fullScreenKey is the four keys every full-screen view answers the same way:
// quit, the help overlay, esc, and the pair that closes the view — its own
// opener pressed again, or `v`. It reports whether the key was one of them.
//
// Views call it from their switch's DEFAULT arm, not before the switch, so a
// view's own bindings are tested first and a shared closer can never shadow a
// local meaning. The graph is why that ordering is the safe one: ⇧space/S
// re-roots there rather than closing, and a pre-switch call handed
// m.keys.Graph would have made it close instead.
//
// own is the view's own opener, which closes it again. The graph has none, so
// it passes a zero Binding — key.Matches never matches one. From the default
// arm that is belt-and-braces rather than load-bearing: keys.Graph is already
// answered by a case above. It is simply the honest spelling of "no own key".
//
// The exits are last in every view now, where main tested them first in three
// of the six. Nothing collides today, and a view whose default arm stopped
// calling this would be caught by TestEveryFullScreenViewAnswersTheSharedKeys
// rather than by a reader.
//
// The sweep reaches its Cancel and Help arms only with the overlay already
// down, because its own pre-switch guard takes the overlay off on any key
// first (the bulk-archive gate must not sit under it). They are not dead in
// the other five.
func (m *Model) fullScreenKey(msg tea.KeyPressMsg, own key.Binding, closeView func()) (tea.Cmd, bool) {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quitOrFlush(), true

	case key.Matches(msg, m.keys.Help):
		m.fullHelp = !m.fullHelp

	case key.Matches(msg, m.keys.Cancel):
		if m.fullHelp {
			m.fullHelp = false
			return nil, true
		}
		closeView()

	case key.Matches(msg, own, m.keys.View):
		closeView()

	default:
		return nil, false
	}
	return nil, true
}

// fullCanvasH is how many rows a full-screen view's own drawing may use: the
// window, less the title and header above it, less whatever the view reserves
// for a band of its own, less the task strip and the status line below.
//
// stripHeight is CALLED rather than passed in because it clamps itself against
// a short window — that clamp is what makes the frames come out right below 24
// rows. reserve sits in the same subtraction the wrappers spelled inline, so
// the arithmetic is unchanged; a future reserve that is not a plain subtracted
// constant does not belong here.
func (m *Model) fullCanvasH(reserve int) int {
	return maxInt(1, m.h-fullTop-reserve-m.stripHeight()-footerH)
}

// windowBands is the pairing that must not disagree: the offset pulled to the
// selection, clamped to what the bands allow, and the slice taken with it. A
// clamp that drifts from its slice is an index panic, so both live here and
// the offset is written back through the pointer.
//
// toSel is the view's own pull-to-selection, which reads the offset off the
// model — so it must be called BEFORE the offset is written, and the `!ok`
// contract in scrollToSel ("a selection the layout no longer has keeps the
// current offset") depends on that order.
//
// Its RESULT is what gets clamped. The views used to clamp before calling it
// instead, which made this slice's safety a property of each caller rather
// than of the code doing the slicing. Both pulls saturate at both ends, so the
// two orders cannot differ; measured over 9,038,458 scrollToSel cases and
// 7,188 scrollGraphToSel cases on real layouts, zero disagreements.
//
// canvasH comes from fullCanvasH and is therefore at least 1. The slice below
// would panic on a negative one, exactly as the inline copies did.
//
// Only views that MATERIALISE every band belong here. The swimlane and the
// roadmap deliberately do not render-then-cut (swimlaneview.go says why), so
// they share the arithmetic above and not this.
func windowBands(scroll *int, bands []string, canvasH int, toSel func() int) []string {
	*scroll = clamp(toSel(), 0, maxInt(0, len(bands)-canvasH))
	if len(bands) <= canvasH {
		return bands
	}
	return bands[*scroll:minInt(len(bands), *scroll+canvasH)]
}

// scrollToSel returns the scroll offset that keeps the selected row on screen,
// computed from the same line the renderer placed it at so the scroll can
// never disagree with the drawing. row resolves the selection to the line it
// sits on (y) and the line scrolling UP must reveal (top: y itself, or the
// group header above it when the row is its group's first — a row is read
// against the cluster / repo / band it belongs to, and stopping one line short
// leaves that header just off the top). A selection the layout no longer has
// keeps the current offset, clamped.
func scrollToSel(scroll, total, canvasH int, row func() (top, y int, ok bool)) int {
	if total <= canvasH {
		return 0
	}
	top, y, ok := row()
	if !ok {
		return clamp(scroll, 0, total-canvasH)
	}
	if top < scroll {
		scroll = top
	}
	if y >= scroll+canvasH {
		scroll = y - canvasH + 1
	}
	return clamp(scroll, 0, total-canvasH)
}

// halfPage is ^u/^d in a view whose window is pinned to the cursor by
// scrollToSel on every frame: nudging the offset alone snaps straight back and
// the key the view's own header advertises does nothing, so half a page of
// ROWS is a cursor move (the table's ^u/^d resolved the same conflict the same
// way). move steps the cursor one row in dir and reports whether it moved;
// where names the axis in the note when it could not move at all. Every
// view's step is monotone along its axis, which is what lets "never moved"
// stand in for "ended where it began"; a step that wrapped around would need
// a before/after comparison instead.
func (m *Model) halfPage(msg tea.KeyPressMsg, canvasH int, move func(dir int) bool, where string) {
	dir := 1
	if msg.String() != "ctrl+d" {
		dir = -1
	}
	moved := false
	for i := maxInt(1, canvasH/2); i > 0; i-- {
		if !move(dir) {
			break
		}
		moved = true
	}
	if !moved {
		m.note("already at the %s of %s", endName(dir), where)
	}
}

// filterCountBit is the status line's claim about the filter. An aggregate
// count must not be made from a verdict the store refused — qErr's only other
// render site is the board's chrome, which these views replace, and "3 hidden"
// from a stale verdict is worse than saying nothing — so a refusal shows
// instead of the number. Empty when there is nothing to say.
func (m *Model) filterCountBit(hidden int) string {
	if m.qErr != "" {
		return m.th.warn.Render("filter refused — this count is from the last good verdict")
	}
	if hidden > 0 {
		return m.th.warn.Render(fmt.Sprintf("%d hidden by the filter", hidden))
	}
	return ""
}

// carryCursorBack lands the board cursor on id when a full-screen walk ended
// there — the contract every full-screen view with task rows keeps on close.
// Only a cursor the USER moved is carried: opening a view on a task its layout
// lacks lands on a fallback row nobody chose, and following THAT back would be
// a silent re-selection. The pin is applied only when the filter would
// otherwise hide the row, so a walk over an unfiltered board leaves no
// permanent exemption behind.
func (m *Model) carryCursorBack(moved bool, id string) {
	if !moved || id == "" {
		return
	}
	if !m.selectID(id, false) {
		m.selectID(id, true)
	}
}

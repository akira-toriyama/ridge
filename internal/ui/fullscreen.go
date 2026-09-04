package ui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

// What the full-screen views (graph, dep map, box overview, roadmap, swimlane)
// share: the frame skeleton and title line above, and below it the
// cursor-pinned scroll, the half-page key, the filter's status-line claim,
// and the cursor carried back to the board on close. Each view keeps its own
// layout, keys and words; these hold the invariants once, so five copies
// cannot drift apart. There is no fullScreenView interface on purpose: the
// layouts share no type, and inventing one would be abstraction for its own
// sake. The graph's scroll follows a two-axis frame of its own and is not a
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

// scrollToSel returns the scroll offset that keeps the selected row on screen,
// computed from the same line the renderer placed it at so the scroll can
// never disagree with the drawing. row resolves the selection to the line it
// sits on (y) and the line scrolling UP must reveal (top: y itself, or the
// group header above it when the row is its group's first — a row is read
// against the cluster / repo / band it belongs to, and stopping one line short
// leaves that header just off the top — the dep map once left
// "── #3  6 nodes · depth 2 ──" there). A selection the layout no longer has
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
// the old before/after comparison back.
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

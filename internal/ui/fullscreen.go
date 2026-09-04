package ui

import (
	"fmt"

	tea "charm.land/bubbletea/v2"
)

// What the full-screen views (dep map, box overview, roadmap, swimlane) share
// below the frame: the cursor-pinned scroll, the half-page key, the filter's
// status-line claim, and the cursor carried back to the board on close. Each
// view keeps its own layout, keys and words; these hold the invariants once,
// so four copies cannot drift apart. The graph is not a client — its scroll
// follows a two-axis frame of its own.

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

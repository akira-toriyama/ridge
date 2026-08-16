package ui

import (
	"time"

	tea "charm.land/bubbletea/v2"
)

// Mouse drag-and-drop.
//
// Four things here are not obvious and all four are load-bearing:
//
//  1. THE THRESHOLD IS CHEBYSHEV, NOT MANHATTAN. A press and a release one cell
//     apart is a CLICK, not a move. Manhattan distance got this wrong on the
//     diagonal — dx=1,dy=1 scores 2 and armed a real drag, and a diagonal twitch
//     is the single most common accidental mouse movement. max(|dx|,|dy|) treats
//     every one-cell neighbour, diagonals included, as a click.
//  2. THE BUTTON IS REMEMBERED, NOT RE-READ. Some terminals do not report the
//     button on motion or release events, so the drag records it at press time.
//     Testing `msg.Button == MouseLeft` on a motion event drops the drag on
//     those terminals.
//  3. ESC CANCELS, AND THE RELEASE THAT FOLLOWS MUST BE A NO-OP. The button is
//     still physically down after Esc, so the release still arrives; it has to
//     be swallowed rather than treated as a drop.
//  4. THE RELEASE DECIDES, NOT THE LAST LANE CROSSED. dropLane used to be
//     sticky, so yanking a card off the board and letting go still committed
//     the move into whatever column the pointer last brushed past — including a
//     release on the title bar. Pulling a card away from the board is the
//     universal escape hatch; it must cancel.
const dragThreshold = 2 // Chebyshev cells

// dragScrollInterval is the repeat rate of the edge auto-scroll. A terminal
// emits motion events only when the pointer MOVES, so parking it at a column
// edge stops delivering events — and the scroll with them. A tick makes holding
// still at the edge keep scrolling, the way every GUI board does.
const dragScrollInterval = 80 * time.Millisecond

// dragScrollMsg is one auto-scroll repeat. `seq` makes it self-cancelling: any
// newer pointer event bumps the counter and every in-flight tick from the old
// position becomes a no-op, so there is never more than one live timer.
type dragScrollMsg struct{ seq int }

type dragState struct {
	armed     bool // a button went down on a card
	moved     bool // the threshold was passed: this is a real drag
	cancelled bool // Esc during the drag; the pending release is a no-op

	id     string
	from   string
	button tea.MouseButton

	pressX, pressY int
	x, y           int
	grabDX, grabDY int

	dropLane string
	dropIdx  int

	scrollDir int // -1 up, +1 down, 0 parked away from an edge
	scrollSeq int
}

func (d *dragState) reset() { *d = dragState{} }

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// inPeek reports whether a point is inside the open side-peek, which owns its
// own clicks and scrolling.
func (m *Model) inPeek(x, y int) bool {
	if !m.peekOpen {
		return false
	}
	px, py, pw, ph := m.peekBox()
	return x >= px && x < px+pw && y >= py && y < py+ph
}

// dropTarget resolves a point to a lane that can actually receive a card: it
// must be inside a column horizontally AND inside that column's card band
// vertically, and it must not be under an overlay that hides the board. The
// chrome rows, the footer, the empty area past the last column and the open
// side-peek are all "off the board".
//
// This is the ONE predicate for "would a release here drop": the renderer asks
// it too (dropLayer, statusLine), so the frame cannot promise a drop that the
// release then cancels.
func (m *Model) dropTarget(x, y int) (string, bool) {
	if m.lay == nil {
		return "", false
	}
	// The peek sits ABOVE the drop indicator (zPeek > zDrop), so a column under
	// it is a column the user cannot see — and the indicator that would mark
	// the slot is painted behind the panel. Releasing there committed a lane
	// change into the invisible board, which is exactly the rule note (4)
	// states. Guarded by POINT, not by column: the strip of a column still
	// visible beside the peek stays a legal drop.
	if m.inPeek(x, y) {
		return "", false
	}
	lane, ok := m.lay.laneAtX(x)
	if !ok {
		return "", false
	}
	c := m.lay.Col(lane)
	if c == nil || y < c.Top || y >= c.Bot {
		return "", false
	}
	return lane, true
}

func (m *Model) onMouseDown(msg tea.MouseClickMsg) tea.Cmd {
	// The board's mouse surface exists only while the board IS the surface.
	// m.lay describes board geometry, so hit-testing it under a modal overlay
	// (filter, edit, add) or a non-board view (table, graph, full help) sends
	// the click to a card nobody can see — reviewed live: a click on the edit
	// overlay silently re-pointed the selection underneath it. The render
	// path composites these layers; the hit path agrees by refusing here, not
	// by growing a parallel hit-test.
	if !m.mouseOn || m.lay == nil || m.fullHelp {
		return nil
	}
	if msg.Button != tea.MouseLeft {
		return nil
	}
	// The slice panel is the one overlay with its own click surface: its
	// inset strip is live wherever the panel is RENDERED (board and table,
	// never the graph) — but only while the board or the panel itself holds
	// the keyboard. A modal (add/edit/filter) or a lifted card must not have
	// the mode switched out from under it by a panel click (review round 2:
	// a click during modeAdd stranded a half-typed title behind an invariant
	// break).
	if m.sliceOpen && msg.X < sliceInsetW && msg.Y >= boardTop && m.view != viewGraph &&
		(m.mode == modeNormal || m.mode == modeSlice) {
		return m.sliceClick(msg.X, msg.Y)
	}
	if m.mode == modeSlice {
		// The panel holds the keyboard; board clicks stay dead until it is
		// closed or unfocused.
		return nil
	}
	if m.view == viewTable {
		// The table's one mouse gesture: sorting by a header cell. Rows and
		// everything else stay keyboard territory, so no drag is ever armed
		// in this view.
		return m.tableClick(msg.X, msg.Y)
	}
	if m.view != viewBoard {
		return nil
	}
	if m.mode == modeMove {
		// A keyboard move is in flight; a click would give the card two owners.
		m.note("finish the keyboard move first (⏎ commit / esc cancel)")
		return nil
	}
	if m.mode != modeNormal {
		return nil
	}
	if m.inPeek(msg.X, msg.Y) {
		return nil
	}

	lane, idx, ok := m.lay.cardAt(msg.X, msg.Y)
	if !ok {
		if lane != "" {
			if i := m.b.LaneIndex(lane); i >= 0 {
				m.curLane = i
			}
		}
		return nil
	}
	c := m.lay.Col(lane)
	if c == nil || idx >= len(c.Tasks) {
		return nil
	}
	t := c.Tasks[idx]

	if i := m.b.LaneIndex(lane); i >= 0 {
		m.curLane = i
	}
	m.curIdx[lane] = idx
	m.syncPeek()

	var box cardBox
	for _, b := range c.Cards {
		if b.Idx == idx {
			box = b
		}
	}
	m.drag = dragState{
		armed: true, id: t.ID, from: lane, button: msg.Button,
		pressX: msg.X, pressY: msg.Y, x: msg.X, y: msg.Y,
		grabDX: msg.X - box.X, grabDY: msg.Y - box.Y,
		dropLane: lane, dropIdx: idx,
	}
	return nil
}

func (m *Model) onMouseMove(msg tea.MouseMotionMsg) tea.Cmd {
	if !m.drag.armed || m.drag.cancelled || m.lay == nil {
		return nil
	}
	if m.view != viewBoard || m.fullHelp || m.mode != modeNormal {
		// An overlay or view change took the screen mid-gesture: the pointer
		// is no longer over what it grabbed, so the drag dies here and the
		// release that follows must be a no-op — completing it would commit a
		// move nobody can see (reproduced under the quick-add modal).
		m.drag.reset()
		return nil
	}
	// Deliberately NOT checking msg.Button: see (2) above.
	m.drag.x, m.drag.y = msg.X, msg.Y
	if !m.drag.moved {
		if maxInt(abs(msg.X-m.drag.pressX), abs(msg.Y-m.drag.pressY)) < dragThreshold {
			return nil
		}
		m.drag.moved = true
	}
	// Which column the pointer is over, for the ghost's target and the
	// auto-scroll hot zone below. The peek is excluded for the same reason
	// dropTarget excludes it: the columns beneath it are not on screen, so
	// tracking one would aim the gesture at something invisible.
	if lane, ok := m.lay.laneAtX(msg.X); ok && !m.inPeek(msg.X, msg.Y) {
		m.drag.dropLane = lane
		m.drag.dropIdx = m.lay.idxAtY(lane, msg.Y)
	}

	// Edge auto-scroll: arm a repeating tick while the pointer sits in the hot
	// zone, and disarm the moment it leaves.
	m.drag.scrollSeq++
	m.drag.scrollDir = 0
	if c := m.lay.Col(m.drag.dropLane); c != nil {
		switch {
		case msg.Y <= c.Top:
			m.drag.scrollDir = -1
		case msg.Y >= c.Bot-1:
			m.drag.scrollDir = +1
		}
	}
	if m.drag.scrollDir == 0 {
		return nil
	}
	m.dragScrollStep()
	return m.dragScrollTick()
}

// dragScrollStep scrolls the hovered column one card, reporting whether it
// actually could. It re-measures immediately so the drop index the status line
// shows matches the rows now under the pointer.
func (m *Model) dragScrollStep() bool {
	c := m.lay.Col(m.drag.dropLane)
	if c == nil || m.drag.scrollDir == 0 {
		return false
	}
	switch {
	case m.drag.scrollDir < 0 && c.Scroll > 0:
		m.scroll[m.drag.dropLane] = c.Scroll - 1
	case m.drag.scrollDir > 0 && c.Hidden > 0:
		m.scroll[m.drag.dropLane] = c.Scroll + 1
	default:
		return false
	}
	m.relayout()
	m.drag.dropIdx = m.lay.idxAtY(m.drag.dropLane, m.drag.y)
	return true
}

func (m *Model) dragScrollTick() tea.Cmd {
	seq := m.drag.scrollSeq
	return tea.Tick(dragScrollInterval, func(time.Time) tea.Msg {
		return dragScrollMsg{seq: seq}
	})
}

func (m *Model) onDragScroll(msg dragScrollMsg) tea.Cmd {
	if !m.drag.armed || !m.drag.moved || m.drag.cancelled {
		return nil
	}
	if msg.seq != m.drag.scrollSeq || m.drag.scrollDir == 0 {
		return nil // superseded by a newer pointer position
	}
	if !m.dragScrollStep() {
		return nil // hit the end of the column: stop ticking
	}
	return m.dragScrollTick()
}

func (m *Model) onMouseUp(msg tea.MouseReleaseMsg) tea.Cmd {
	if !m.drag.armed {
		return nil
	}
	if m.view != viewBoard || m.fullHelp || m.mode != modeNormal {
		// Same rule as onMouseMove: a release under an overlay or in another
		// view must never commit into the invisible board.
		m.drag.reset()
		return nil
	}
	if m.drag.cancelled {
		m.drag.reset()
		return nil
	}
	if !m.drag.moved {
		// A click, not a drag. Selection already happened on press.
		m.note("selected %s — drag it, or press ⏎ for move mode", m.drag.id)
		m.drag.reset()
		return nil
	}

	id, from := m.drag.id, m.drag.from
	to, onBoard := m.dropTarget(msg.X, msg.Y)
	if !onBoard {
		m.drag.reset()
		m.note("released off the board — %s stayed in %s", id, from)
		return nil
	}
	// The RELEASE decides where it lands, not the last lane the pointer brushed.
	dropIdx := m.lay.idxAtY(to, msg.Y)
	m.drag.reset()

	moved, cmd, err := m.commitMove(id, from, to, dropIdx)
	if err != nil {
		m.fail("%v", err)
		return nil
	}
	switch {
	case !moved:
		m.note("%s did not move — dropped back into its own slot", id)
	case from == to:
		m.note("%s repositioned in %s", id, to)
	default:
		m.note("%s: %s → %s (dropped)", id, from, to)
	}
	return cmd
}

func (m *Model) onWheel(msg tea.MouseWheelMsg) {
	// fullHelp covers the whole screen (zHelp > zPeek) and the graph view
	// never renders the peek — in both, `inPeek` would say yes to a panel
	// that is not on screen.
	if !m.mouseOn || m.fullHelp {
		return
	}
	// The peek scrolls in EVERY mode it is visible in — the edit overlay
	// deliberately opens it (enterEdit), and review confirmed the modality
	// guard below made a long body unreadable while editing. Scrolling the
	// peek commits nothing; it is not the board's hit surface.
	if m.view != viewGraph && m.inPeek(msg.X, msg.Y) {
		switch msg.Button {
		case tea.MouseWheelUp:
			m.vp.ScrollUp(3)
		case tea.MouseWheelDown:
			m.vp.ScrollDown(3)
		}
		return
	}
	// The slice panel scrolls under the wheel wherever it is rendered. The
	// cursor deliberately stays put — like a board column, a panel scrolled
	// away from its cursor is a legitimate state (the next arrow re-pulls).
	if m.sliceOpen && msg.X < sliceInsetW && msg.Y >= boardTop && m.view != viewGraph {
		switch msg.Button {
		case tea.MouseWheelUp:
			m.sliceOff--
		case tea.MouseWheelDown:
			m.sliceOff++
		}
		off, _, _ := m.sliceViewport(len(m.sliceRows()))
		m.sliceOff = off
		return
	}
	// The BOARD wheel needs the board on screen and no modal overlay over
	// it. modeMove and modeSlice stay scrollable — a lifted card is not an
	// overlay, and the panel sits BESIDE the columns, not over them.
	if m.mode != modeNormal && m.mode != modeMove && m.mode != modeSlice {
		return
	}
	if m.view != viewBoard || m.lay == nil {
		// Table and graph have no board columns on screen to scroll.
		return
	}
	lane, ok := m.lay.laneAtX(msg.X)
	if !ok {
		return
	}
	c := m.lay.Col(lane)
	if c == nil {
		return
	}
	switch msg.Button {
	case tea.MouseWheelUp:
		m.scroll[lane] = maxInt(0, c.Scroll-1)
	case tea.MouseWheelDown:
		// Only scroll if there is something below the fold. Clamping to
		// len(tasks)-1 instead let a column whose cards all fit be scrolled
		// until the top one was simply gone.
		if c.Hidden > 0 {
			m.scroll[lane] = c.Scroll + 1
		}
	}
}

// cancelDrag is Esc while a button is down. The drag stops immediately but stays
// armed so the release that inevitably follows is swallowed.
func (m *Model) cancelDrag() bool {
	if !m.drag.armed || m.drag.cancelled {
		return false
	}
	id := m.drag.id
	m.drag.cancelled, m.drag.moved = true, false
	m.drag.scrollDir, m.drag.scrollSeq = 0, m.drag.scrollSeq+1
	m.note("drag cancelled — %s stayed in %s", id, m.drag.from)
	return true
}

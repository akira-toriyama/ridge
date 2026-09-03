package ui

import (
	tea "charm.land/bubbletea/v2"
)

// The board cursor: which lane and card is selected, how it moves, and the
// jump-to-blocker stack. Nothing here writes to the store — the two quick
// reorders (K/J and H/L) apply through commitMove like every other move.

func (m *Model) countVisible() int {
	n := 0
	for _, l := range m.b.Lanes() {
		n += len(m.cols[l.Name])
	}
	return n
}

func (m *Model) setPos(i int) {
	if m.view == viewTable {
		m.tableIdx = clamp(i, 0, maxInt(0, len(m.tableRows())-1))
		m.syncPeek()
		return
	}
	m.curIdx[m.curLaneName()] = clamp(i, 0, maxInt(0, len(m.curTasks())-1))
	m.ensureVisible()
	m.syncPeek()
}

// moveCursor walks the grid. Moving between columns keeps the row position when
// it exists, which is what makes a kanban feel like a grid rather than a set of
// unrelated lists.
func (m *Model) moveCursor(dx, dy int) {
	if m.view == viewTable {
		if dy != 0 {
			m.tableIdx = clamp(m.tableIdx+dy, 0, maxInt(0, len(m.tableRows())-1))
			m.syncPeek()
		}
		return
	}
	if dy != 0 {
		m.setPos(m.curPos() + dy)
		return
	}
	if dx == 0 {
		return
	}
	want := m.curPos()
	i := m.curLane + dx
	if i < 0 || i >= len(m.b.Lanes()) {
		return
	}
	// One lane per keypress, landing on an EMPTY lane too (clamp snaps the
	// index to 0 there): you must be able to drop into one, and a lane you
	// cannot focus is a lane you cannot drop into. An earlier skip-empty
	// design scanned onward — that scan is deliberately gone.
	m.curLane = i
	m.curIdx[m.laneName(i)] = clamp(want, 0, len(m.cols[m.laneName(i)])-1)
	m.ensureVisible()
	m.syncPeek()
}

// selectID moves the cursor onto a task, pinning it past the filter when asked
// — a jump that lands nowhere is worse than no jump.
func (m *Model) selectID(id string, pin bool) bool {
	t := m.b.Task(id)
	if t == nil {
		return false
	}
	if pin {
		m.pinned[id] = true
		m.recompute()
	}
	for i, l := range m.b.Lanes() {
		for j, x := range m.cols[l.Name] {
			if x.ID == id {
				m.curLane, m.curIdx[l.Name] = i, j
				if m.view == viewTable {
					for k, r := range m.tableRows() {
						if r.ID == id {
							m.tableIdx = k
						}
					}
				}
				m.ensureVisible()
				m.syncPeek()
				return true
			}
		}
	}
	return false
}

// jumpToBlocker is the one interactive dep feature a static drawing cannot do.
// The real board's longest chain is 5 edges, so two presses reach any root
// blocker.
func (m *Model) jumpToBlocker() {
	t := m.curTask()
	if t == nil {
		return
	}
	blockers := m.g.BlockedBy(t.ID)
	if len(blockers) == 0 {
		m.note("%s is not blocked", t.ID)
		return
	}
	target := blockers[0]
	if !m.g.Known(target) {
		m.fail("%s depends on %s, which is not on this board", t.ID, target)
		return
	}
	m.jumpStack = append(m.jumpStack, t.ID)
	pinned := ""
	if !m.selectID(target, false) {
		m.selectID(target, true)
		pinned = " (pinned past the filter)"
	}
	m.note("→ %s (blocker %d/%d of %s)%s  ·  < to come back",
		target, 1, len(blockers), t.ID, pinned)
}

func (m *Model) jumpBack() {
	if len(m.jumpStack) == 0 {
		m.note("jump stack empty")
		return
	}
	id := m.jumpStack[len(m.jumpStack)-1]
	m.jumpStack = m.jumpStack[:len(m.jumpStack)-1]
	// Pin only when the target is genuinely hidden, the way jumpToBlocker does.
	// A pin is a permanent filter exemption plus a "+N pinned by jump" chip,
	// and pins are cleared only when the effective query empties — so pinning
	// unconditionally on an unfiltered board leaked an exemption that then
	// defied a filter typed later.
	if !m.selectID(id, false) {
		m.selectID(id, true)
	}
	m.note("← %s (%d left on the stack)", id, len(m.jumpStack))
}

// cycleLane moves a task one lane over without entering move mode, appending it
// after the last card the filter SHOWS in the destination (boardInsertIndex's
// contract — not past cards the filter is hiding). A destination the filter
// EMPTIED puts the card on the lane's TOP: the slot a mouse drop into that
// same emptied column takes, kept identical so the two gestures cannot
// disagree about one visible state. It must go through commitMove: a direct
// MoveTo mutated the board before enqueuePersist could refuse it, so a
// rollback in flight rejected the write but not the gesture.
func (m *Model) cycleLane(d int) tea.Cmd {
	t := m.curTask()
	if t == nil {
		return nil
	}
	i := m.b.LaneIndex(t.Status) + d
	if i < 0 || i >= len(m.b.Lanes()) {
		m.note("no lane that way")
		return nil
	}
	dest := m.laneName(i)
	_, cmd, err := m.commitMove(t.ID, t.Status, dest, len(m.cols[dest]))
	if err != nil {
		m.fail("%v", err)
		return nil
	}
	// No note: the card is now in the other lane, with the cursor on it.
	return cmd
}

// quickReorder is shift+K / shift+J: nudge within the lane without the ceremony
// of move mode.
func (m *Model) quickReorder(d int) tea.Cmd {
	t := m.curTask()
	if t == nil {
		return nil
	}
	if m.view == viewTable && m.tableSort > sortCanonical {
		// GitHub's rule: a sorted table cannot be hand-reordered. The write
		// would land fine, but the sorted view wouldn't move — a nudge that
		// changes nothing on screen reads as a dead key.
		m.note("sorted by %s — reordering needs canonical order (o cycles back, or click lane)", m.tableSort)
		return nil
	}
	vis := m.cols[t.Status]
	from := -1
	for i, x := range vis {
		if x.ID == t.ID {
			from = i
		}
	}
	if from < 0 {
		return nil
	}
	to := from + d
	if to < 0 || to >= len(vis) {
		m.note("%s is already at the %s of %s", t.ID, endName(d), t.Status)
		return nil
	}
	moved, cmd, err := m.commitMove(t.ID, t.Status, t.Status, to+boolToInt(d > 0))
	if err != nil {
		m.fail("%v", err)
		return nil
	}
	if !moved {
		// This one stays: nothing happened, and nothing NOT happening is
		// exactly what the screen cannot show.
		m.note("%s did not move", t.ID)
		return nil
	}
	// The card visibly changed places, so there is nothing left to report.
	return cmd
}

func endName(d int) string {
	if d < 0 {
		return "top"
	}
	return "bottom"
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

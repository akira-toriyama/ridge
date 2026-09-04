package ui

import (
	"fmt"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	"github.com/akira-toriyama/ridge/internal/board"
)

// Keyboard move mode — GitHub Projects' board gesture, which transplants to a
// TUI almost too well: enter lifts the card, arrows place it, enter commits,
// esc restores.
//
// commitMove at the bottom of this file is the SINGLE mutation path for every
// reorder gesture in the app (move mode, shift+J/K, H/L lane cycling, and a
// mouse drop), which is what keeps the two index translations applied exactly
// once each — and the rollingBack refusal applied to every reorder gesture.

func (m *Model) enterMove() {
	if m.drag.armed {
		// The mirror of onMouseDown's guard. Without it a card could be lifted
		// by the keyboard while the mouse still held it: the release commits one
		// move and the following Enter commits a second.
		m.note("a mouse drag is in flight — release it first")
		return
	}
	t := m.curTask()
	if t == nil {
		m.note("nothing to move in %s", m.curLaneName())
		return
	}
	if m.view == viewTable {
		m.note("move mode is a board gesture — press v for the board")
		return
	}
	m.mode = modeMove
	m.moveID, m.moveFrom = t.ID, t.Status
	m.dropLane, m.dropIdx = t.Status, m.curPos()
	// followDrop() walks m.curLane/m.curIdx along with the drop target, so cancel
	// has to be able to put them back. "esc restores" has to mean the SELECTION
	// too, not just the board: leaving the cursor two lanes over means the next
	// d / x / Enter silently acts on a different task.
	m.moveCurLane = m.curLane
	m.moveCurIdx = make(map[string]int, len(m.curIdx))
	for k, v := range m.curIdx {
		m.moveCurIdx[k] = v
	}
	m.note("MOVE %s — arrows place it, ⏎ commits, esc restores", t.ID)
}

// cancelMove restores both the board (nothing was mutated) and the cursor.
func (m *Model) cancelMove() {
	m.mode = modeNormal
	// Do not overwrite a refusal the user has not seen yet. A persist failure
	// that landed mid-gesture rides the MOVE row as a ⚠ suffix; clobbering it
	// here was the second half of "the rollback lands silently" — the warning
	// appeared for exactly as long as the card stayed lifted, and esc erased it.
	if !m.statusErr {
		m.note("move cancelled — %s stayed in %s", m.moveID, m.moveFrom)
	}
	m.curLane = m.moveCurLane
	if m.moveCurIdx != nil {
		m.curIdx = m.moveCurIdx
	}
	m.moveID, m.moveCurIdx = "", nil
	m.ensureVisible()
	m.syncPeek()
	m.releaseHeldVerdict()
}

func (m *Model) onMoveKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	// `?` must work here: the move-mode title row advertises it, and the help
	// overlay is the only listing of the K/J/H/L extremes.
	case key.Matches(msg, m.keys.Help):
		m.fullHelp = !m.fullHelp
		return nil

	case key.Matches(msg, m.keys.Cancel):
		// The overlay is on top, so it is what esc takes off first — same
		// ordering as the board's and the graph's Cancel.
		if m.fullHelp {
			m.fullHelp = false
			return nil
		}
		m.cancelMove()
		return nil

	case key.Matches(msg, m.keys.Commit):
		id, from, to, di := m.moveID, m.moveFrom, m.dropLane, m.dropIdx
		moved, cmd, err := m.commitMove(id, from, to, di)
		if err != nil {
			// The move state must still be intact here: a refused commit has
			// to restore the lift-time SELECTION exactly as esc would, or the
			// cursor stays wherever followDrop parked it — two lanes over,
			// on a task the next keystroke silently acts on. cancelMove also
			// skips its own note while statusErr shows the refusal.
			m.fail("%v", err)
			m.cancelMove()
			return nil
		}
		m.mode, m.moveID, m.moveCurIdx = modeNormal, "", nil
		m.releaseHeldVerdict() // only after the commit consumed its slot
		switch {
		case !moved:
			m.note("%s did not move — that is where it already was", id)
		case from == to:
			m.note("%s repositioned in %s", id, to)
		default:
			m.note("%s: %s → %s", id, from, to)
		}
		return cmd

	case key.Matches(msg, m.keys.Quit):
		return m.quitOrFlush()

	// To the extremes: top / bottom of the column, leftmost / rightmost lane.
	// K/J/H/L primary (uppercase = "all the way" vs lowercase's one step),
	// GitHub's ctrl+arrows kept as silent aliases.
	case key.Matches(msg, m.keys.MoveTop):
		m.dropIdx = 0
	case key.Matches(msg, m.keys.MoveBottom):
		m.dropIdx = m.dropSpan(m.dropLane)
	case key.Matches(msg, m.keys.MoveFirst):
		m.setDropLane(0)
	case key.Matches(msg, m.keys.MoveLast):
		m.setDropLane(len(m.b.Lanes()) - 1)

	case key.Matches(msg, m.keys.Up):
		m.dropIdx = maxInt(0, m.dropIdx-1)
	case key.Matches(msg, m.keys.Down):
		m.dropIdx = minInt(m.dropSpan(m.dropLane), m.dropIdx+1)
	case key.Matches(msg, m.keys.Left):
		m.shiftDropLane(-1)
	case key.Matches(msg, m.keys.Right):
		m.shiftDropLane(+1)
	}
	m.followDrop()
	return nil
}

// dropSpan is the largest insertion index for a lane. The moving card is still
// DISPLAYED in its source lane (nothing reflows under the cursor), so both the
// source and a foreign lane offer indices 0..len — the difference is undone by
// AdjustDropIndex at commit time, not here.
func (m *Model) dropSpan(lane string) int { return len(m.cols[lane]) }

func (m *Model) shiftDropLane(d int) { m.setDropLane(m.b.LaneIndex(m.dropLane) + d) }

func (m *Model) setDropLane(i int) {
	if i < 0 || i >= len(m.b.Lanes()) {
		return
	}
	m.dropLane = m.laneName(i)
	m.dropIdx = clamp(m.dropIdx, 0, m.dropSpan(m.dropLane))
}

// followDrop keeps the viewport tracking the drop target, so placing a card
// into an off-screen lane scrolls the board to it.
func (m *Model) followDrop() {
	if i := m.b.LaneIndex(m.dropLane); i >= 0 {
		m.curLane = i
	}
	m.curIdx[m.dropLane] = clamp(m.dropIdx, 0, maxInt(0, len(m.cols[m.dropLane])-1))
	m.ensureVisible()
}

// commitMove is the ONE mutation path for every reorder gesture — move mode,
// shift+J/K, H/L lane cycling, and mouse drop all land here. It applies the
// move to the board (the optimistic half) and returns the tea.Cmd that records it in the store
// (the persist half); it reports whether the board actually changed, so a
// clamped or no-op gesture can say so instead of claiming a reposition that
// never happened.
//
// dispIdx is an insertion index measured against the destination column AS
// DISPLAYED: it still counts the moving card (nothing reflows under the cursor)
// and it counts only tasks the filter lets through. Both facts have to be
// undone before the board can be told anything.
//
// The card's own slot is NOT a parameter: the board can recompose under a held
// gesture — the post-persist reconcile and the rollback re-read both land as
// async messages, and neither cancels a lifted card (a drag, too, survives any
// recompose that keeps its card in the grabbed lane; recompute cancels only a
// drag whose card LEFT it) — so an index a
// caller recorded at press/lift time can be stale by commit time, and a stale
// index shifts AdjustDropIndex's boundary: a drop into the card's own slot
// writes one slot off (t-raw1). The slot is therefore derived here, against
// the same displayed columns dispIdx was just measured against.
func (m *Model) commitMove(id, from, to string, dispIdx int) (moved bool, cmd tea.Cmd, err error) {
	if m.rollingBack {
		// Refuse the GESTURE, not just its enqueue: every caller follows a
		// successful commit with its own note(), which would overwrite
		// enqueuePersist's refusal and tell the user the move worked right
		// before the rollback re-read yanks it back (t-74y3). The error
		// path is one every caller already handles first.
		return false, nil, fmt.Errorf("move %s dropped — the store refused the last write, rolling back", id)
	}
	fromIdx := displayIndex(m.cols[from], id)
	if fromIdx < 0 && m.b.IndexIn(from, id) < 0 {
		// The card LEFT the lane the gesture grabbed it in — moved or closed
		// by a re-read. Committing would silently yank back a change the user
		// was never shown (the class t-74y3 exists to prevent), so refuse and
		// let them re-issue the gesture against what is now on screen.
		//
		// A card the FILTER hid mid-gesture is deliberately not refused: it is
		// still in the lane, and a cross-lane drop of it is exactly what the
		// user asked for.
		return false, nil, fmt.Errorf("%s is no longer shown in %s — the board changed under the gesture, nothing moved", id, from)
	}
	// A hidden-but-present card also needs NO self-slot correction: the
	// displayed column does not count it, so dispIdx was measured against a
	// column without it — the exact post-removal shape MoveTo expects.
	adj := board.AdjustDropIndex(from == to && fromIdx >= 0, fromIdx, dispIdx)

	visNoSelf := withoutID(m.cols[to], id)
	fullNoSelf := withoutID(m.b.LaneTasks(to), id)
	boardIdx := boardInsertIndex(fullNoSelf, visNoSelf, adj)

	// A drop into the slot the card already occupies must not touch the store.
	// furrow's contract is that positional bookkeeping does not advance
	// `updated` — that field is the staleness signal `lint` reads.
	if from == to && m.b.IndexIn(to, id) == boardIdx {
		m.selectID(id, false)
		return false, nil, nil
	}

	renumbered, err := m.b.MoveTo(id, to, boardIdx)
	if err != nil {
		return false, nil, err
	}
	m.recompute()
	m.selectID(id, false)
	if len(renumbered) > 0 {
		m.note("respaced %s (%d neighbours renumbered)", to, len(renumbered))
	}
	return true, m.persistPlacement(id, to), nil
}

// displayIndex is the card's slot in a displayed (filtered) column, -1 when
// the column does not currently show it.
func displayIndex(ts []*board.Task, id string) int {
	for i, t := range ts {
		if t.ID == id {
			return i
		}
	}
	return -1
}

func withoutID(ts []*board.Task, id string) []*board.Task {
	out := make([]*board.Task, 0, len(ts))
	for _, t := range ts {
		if t.ID != id {
			out = append(out, t)
		}
	}
	return out
}

// boardInsertIndex translates an insertion index in a FILTERED column into one
// in the full lane: land immediately before whichever visible task currently
// holds that slot. Without this, dropping "second from the top" of a filtered
// column would silently mean "second from the top" of the unfiltered lane.
//
// The two edges are where a naive fallback lies about the gesture:
//   - nothing visible at all (an empty lane, or one the filter emptied): the
//     gesture said TOP, so appending to the real BOTTOM is the wrong answer.
//   - past the last visible card: that means "after the last card you can SEE",
//     not "after cards the filter is hiding from you".
func boardInsertIndex(full, vis []*board.Task, visIdx int) int {
	if len(vis) == 0 {
		return 0
	}
	if visIdx >= len(vis) {
		last := vis[len(vis)-1].ID
		for i, t := range full {
			if t.ID == last {
				return i + 1
			}
		}
		return len(full)
	}
	target := vis[visIdx].ID
	for i, t := range full {
		if t.ID == target {
			return i
		}
	}
	return len(full)
}

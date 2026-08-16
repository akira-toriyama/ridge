package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// The board can recompose while a gesture is held: the post-persist reconcile
// and the rollback re-read both land as async messages, and neither cancels a
// drag or a lifted card. The slot recorded at press time is then stale, and
// stale arithmetic shifted AdjustDropIndex's remove-then-insert boundary — a
// drop back into the card's own slot was written one slot down (t-raw1). These
// tests hold a drag across a recompose; commitMove must translate against the
// columns as displayed at RELEASE time, the same snapshot dispIdx is measured
// against.

// staleLane is one populated lane, ready = [a, x, b, c], with room for an
// external writer to reorder it under a held drag.
func staleLane() *board.Board {
	mk := func(id string, prio int) *board.Task {
		return &board.Task{ID: id, Title: "card " + id, Status: "ready", Priority: prio}
	}
	return board.NewBoard([]*board.Task{mk("a", 100), mk("x", 200), mk("b", 300), mk("c", 400)})
}

// readyBox returns the box of the card displayed at ready[idx].
func readyBox(t *testing.T, m *Model, idx int) cardBox {
	t.Helper()
	c := m.lay.Col("ready")
	if c == nil {
		t.Fatal("no ready column on the board")
	}
	for _, b := range c.Cards {
		if b.Idx == idx {
			return b
		}
	}
	t.Fatalf("ready[%d] is not on screen", idx)
	return cardBox{}
}

func TestDropIntoOwnSlotSurvivesAMidDragRecompose(t *testing.T) {
	m := New(memstore.NewWith(staleLane()), Options{})
	m.w, m.h = 140, 40
	m.recompute()
	m.relayout()

	// Grab x, displayed second.
	box := readyBox(t, m, 1)
	if got := m.lay.Col("ready").Tasks[1].ID; got != "x" {
		t.Fatalf("fixture: ready[1] shows %s, want x", got)
	}
	m.Update(tea.MouseClickMsg{X: box.X + 3, Y: box.Y + 1, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: box.X + 3 + dragThreshold, Y: box.Y + 1 + dragThreshold, Button: tea.MouseLeft})
	if !m.drag.moved {
		t.Fatal("the pointer move did not arm a drag")
	}

	// The reconcile lands mid-drag: another writer sent a to the bottom, so x
	// is now DISPLAYED first.
	if _, err := m.b.MoveTo("a", "ready", 3); err != nil {
		t.Fatal(err)
	}
	m.recompute()
	m.relayout()
	if got := m.lay.Col("ready").Tasks[0].ID; got != "x" {
		t.Fatalf("recompose: ready[0] shows %s, want x", got)
	}

	// Release just below x's own card — insertion index 1, the vacated-slot
	// no-op every kanban promises. With the press-time slot (1) still in the
	// arithmetic this read as "below somebody else" and wrote [b, x, c, a].
	below := readyBox(t, m, 1)
	m.Update(tea.MouseReleaseMsg{X: below.X + 3, Y: below.Y, Button: tea.MouseLeft})

	if got := strings.Join(ids(m.b.LaneTasks("ready")), ","); got != "x,b,c,a" {
		t.Errorf("ready = %s, want x,b,c,a — the drop into the card's own slot moved it", got)
	}
}

// When the recompose removed the card from the column the gesture grabbed it
// in — moved lanes, closed, filtered out by a re-read — the gesture's premise
// is gone: committing would address a board the user was not shown. It must
// refuse, mutate nothing, and enqueue nothing.
func TestCommitMoveRefusesWhenTheColumnNoLongerShowsTheCard(t *testing.T) {
	m := New(memstore.NewWith(staleLane()), Options{})
	m.w, m.h = 140, 40
	m.recompute()
	m.relayout()

	if _, err := m.b.MoveTo("x", "backlog", 0); err != nil {
		t.Fatal(err)
	}
	m.recompute()

	moved, cmd, err := m.commitMove("x", "ready", "ready", 1)
	if err == nil {
		t.Fatal("commitMove accepted a gesture whose card left the column")
	}
	if moved || cmd != nil || len(m.pending) != 0 {
		t.Errorf("refusal must be total: moved=%v cmd=%v pending=%d", moved, cmd != nil, len(m.pending))
	}
	if got := strings.Join(ids(m.b.LaneTasks("ready")), ","); got != "a,b,c" {
		t.Errorf("ready = %s, want a,b,c untouched", got)
	}
	if got := m.b.Task("x").Status; got != "backlog" {
		t.Errorf("x is in %s, want backlog untouched", got)
	}
}

// labeledLane is staleLane with a,b,c labelled so a filter can hide x while it
// stays in the lane — the state a debounced verdict landing mid-drag produces.
func labeledLane() *board.Board {
	mk := func(id string, prio int, labels ...string) *board.Task {
		return &board.Task{ID: id, Title: "card " + id, Status: "ready", Priority: prio, Labels: labels}
	}
	return board.NewBoard([]*board.Task{
		mk("a", 100, "ui"), mk("x", 200), mk("b", 300, "ui"), mk("c", 400, "ui"),
	})
}

// A card the filter hid is still IN its lane: a cross-lane drop of it is
// exactly what the user asked for, and the slot arithmetic never consults the
// source slot on a lane change. Refusing it would regress a working gesture
// — pin the commit.
func TestHiddenCardCrossLaneDropStillCommits(t *testing.T) {
	m := New(memstore.NewWith(labeledLane()), Options{})
	m.w, m.h = 140, 40
	m.applyFilter("label:ui")
	m.recompute()
	m.relayout()
	if displayIndex(m.cols["ready"], "x") != -1 {
		t.Fatal("fixture: the filter must hide x")
	}

	moved, cmd, err := m.commitMove("x", "ready", "backlog", 0)
	if err != nil || !moved || cmd == nil {
		t.Fatalf("a hidden card's cross-lane drop must commit: moved=%v cmd=%v err=%v", moved, cmd != nil, err)
	}
	if got := m.b.Task("x").Status; got != "backlog" {
		t.Errorf("x is in %s, want backlog", got)
	}
	if got := ids(m.b.LaneTasks("backlog")); len(got) == 0 || got[0] != "x" {
		t.Errorf("backlog = %v, want x on top", got)
	}
}

// Same lane, hidden card: the displayed column does not count the card, so
// dispIdx is already the post-removal shape and needs NO self-slot correction.
// Dropping below the last visible card must land there — not one slot short,
// which is where the press-time correction used to put it.
func TestHiddenCardSameLaneDropLandsWhereTheMarkerPointed(t *testing.T) {
	m := New(memstore.NewWith(labeledLane()), Options{})
	m.w, m.h = 140, 40
	m.applyFilter("label:ui")
	m.recompute()
	m.relayout()

	moved, _, err := m.commitMove("x", "ready", "ready", 3) // below c, as displayed
	if err != nil || !moved {
		t.Fatalf("moved=%v err=%v", moved, err)
	}
	if got := strings.Join(ids(m.b.LaneTasks("ready")), ","); got != "a,b,c,x" {
		t.Errorf("ready = %s, want a,b,c,x — the drop below the last visible card landed elsewhere", got)
	}
}

// The keyboard twin: a lifted card whose lane a re-read emptied under it. The
// refused Enter must restore the lift-time selection exactly as esc would —
// clearing the move state BEFORE the commit left the cursor wherever
// followDrop parked it, silently re-pointing the next keystroke.
func TestRefusedMoveCommitRestoresTheLiftTimeSelection(t *testing.T) {
	m := New(memstore.NewWith(staleLane()), Options{})
	m.w, m.h = 140, 40
	m.recompute()
	m.relayout()

	m.curLane = m.b.LaneIndex("ready")
	m.setPos(1) // x
	m.enterMove()
	liftLane, liftIdx := m.moveCurLane, m.moveCurIdx["ready"]
	m.Update(tea.KeyPressMsg{Code: tea.KeyLeft}) // park the drop (and the cursor) in another lane

	if _, err := m.b.MoveTo("x", "backlog", 0); err != nil {
		t.Fatal(err)
	}
	m.recompute()
	m.relayout()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})

	if !m.statusErr || !strings.Contains(m.status, "no longer shown") {
		t.Errorf("the refusal did not reach the status line: statusErr=%v status=%q", m.statusErr, m.status)
	}
	if m.mode != modeNormal || m.moveID != "" {
		t.Errorf("the lift must end with the refusal: mode=%v moveID=%q", m.mode, m.moveID)
	}
	if m.curLane != liftLane || m.curIdx["ready"] != liftIdx {
		t.Errorf("selection = lane %d idx %d, want the lift-time lane %d idx %d restored",
			m.curLane, m.curIdx["ready"], liftLane, liftIdx)
	}
}

// The refusal through the real gesture: grab a card, let a re-read move it to
// another lane, release. The store's truth must stand — committing would yank
// the external move back — and the refusal must reach the status line.
func TestReleaseAfterTheCardLeftItsLaneRefusesInsteadOfYankingItBack(t *testing.T) {
	m := New(memstore.NewWith(staleLane()), Options{})
	m.w, m.h = 140, 40
	m.recompute()
	m.relayout()

	box := readyBox(t, m, 1)
	m.Update(tea.MouseClickMsg{X: box.X + 3, Y: box.Y + 1, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: box.X + 3 + dragThreshold, Y: box.Y + 1 + dragThreshold, Button: tea.MouseLeft})
	if !m.drag.moved {
		t.Fatal("the pointer move did not arm a drag")
	}

	if _, err := m.b.MoveTo("x", "backlog", 0); err != nil {
		t.Fatal(err)
	}
	m.recompute()
	m.relayout()

	rel := readyBox(t, m, 0)
	m.Update(tea.MouseReleaseMsg{X: rel.X + 3, Y: rel.Y + 1, Button: tea.MouseLeft})

	if !m.statusErr || !strings.Contains(m.status, "no longer shown") {
		t.Errorf("the refusal did not reach the status line: statusErr=%v status=%q", m.statusErr, m.status)
	}
	if got := m.b.Task("x").Status; got != "backlog" {
		t.Errorf("x is in %s, want backlog — the release yanked the external move back", got)
	}
	if got := strings.Join(ids(m.b.LaneTasks("ready")), ","); got != "a,b,c" {
		t.Errorf("ready = %s, want a,b,c untouched", got)
	}
}

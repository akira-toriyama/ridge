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
	if moved || cmd != nil {
		t.Errorf("refusal must be total: moved=%v cmd=%v", moved, cmd != nil)
	}
	if got := strings.Join(ids(m.b.LaneTasks("ready")), ","); got != "a,b,c" {
		t.Errorf("ready = %s, want a,b,c untouched", got)
	}
	if got := m.b.Task("x").Status; got != "backlog" {
		t.Errorf("x is in %s, want backlog untouched", got)
	}
}

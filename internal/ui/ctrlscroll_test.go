package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// ^d/^u scroll whatever the user is looking at: the open peek wins, then the
// table, then the focused board column. They used to be peek-only — a silent
// dead key in the other two states while the help advertised a plain "scroll"
// (t-84r1) — and a gesture that cannot move must say so, not do nothing.

func ctrlD() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'd', Mod: tea.ModCtrl} }
func ctrlU() tea.KeyPressMsg { return tea.KeyPressMsg{Code: 'u', Mod: tea.ModCtrl} }

func TestCtrlScrollMovesTheOpenPeek(t *testing.T) {
	m := New(memstore.New(), Options{Peek: true})
	m.Update(tea.WindowSizeMsg{Width: 240, Height: 40})
	m.Update(ctrlD())
	if m.vp.YOffset() == 0 {
		t.Error("ctrl+d did not scroll the open peek")
	}
	m.Update(ctrlU())
	if m.vp.YOffset() != 0 {
		t.Error("ctrl+u did not scroll the peek back to the top")
	}
}

func TestCtrlScrollMovesTheFocusedColumnWhenThePeekIsClosed(t *testing.T) {
	// A tall frame, so the column shows enough cards for the HALF to be a
	// real number: at h=30 only 2-3 cards fit and the step degenerates to 1,
	// which let step and relayout mutants survive — the half-page claim went
	// untested.
	m := New(memstore.NewWith(board.NewBoard(scaledTasks(60))), Options{})
	m.Update(tea.WindowSizeMsg{Width: 240, Height: 60})

	lane := m.curLaneName()
	c := m.lay.Col(lane)
	if c == nil || c.Hidden == 0 {
		t.Fatalf("fixture: column %s must overflow (hidden=%v)", lane, c)
	}
	if len(c.Cards) < 4 {
		t.Fatalf("fixture: need >= 4 visible cards for a real half page, got %d", len(c.Cards))
	}
	wantStep := len(c.Cards) / 2
	m.Update(ctrlD())
	if got := m.lay.Col(lane).Scroll; got != wantStep {
		t.Errorf("ctrl+d scrolled %d cards, want half the %d visible = %d", got, len(c.Cards), wantStep)
	}
	m.Update(ctrlU())
	if got := m.lay.Col(lane).Scroll; got != 0 {
		t.Errorf("ctrl+u did not scroll back to the top, at %d", got)
	}

	// At the very top, ctrl+u must SAY it cannot move.
	m.status = ""
	m.Update(ctrlU())
	if !strings.Contains(m.status, "already at the top") {
		t.Errorf("a ctrl+u that cannot move must say so, status=%q", m.status)
	}

	// And the BOTTOM must speak too: the Hidden>0 guard is what separates a
	// real move from a clamped one, and dropping it revived the silent dead
	// key at the bottom while every other assert stayed green.
	for i := 0; i < 80; i++ {
		m.Update(ctrlD())
	}
	m.status = ""
	m.Update(ctrlD())
	if !strings.Contains(m.status, "already at the bottom") {
		t.Errorf("a ctrl+d that cannot move must say so, status=%q", m.status)
	}
	if m.lay.Col(lane).Hidden != 0 {
		t.Error("the column claims the bottom while still hiding cards")
	}
}

// The open peek outranks the table — and it is genuinely on screen there
// (table.go renders peekLayer), so this is not an invisible scroll. The
// precedence was otherwise untested: narrowing the peek case to board view
// survived the rest of the suite.
func TestCtrlScrollPrefersTheOpenPeekOverTheTable(t *testing.T) {
	m := New(memstore.New(), Options{Table: true, Peek: true})
	m.Update(tea.WindowSizeMsg{Width: 240, Height: 40})
	before := m.tableIdx
	m.Update(ctrlD())
	if m.vp.YOffset() == 0 {
		t.Error("ctrl+d did not scroll the open peek in the table view")
	}
	if m.tableIdx != before {
		t.Errorf("ctrl+d moved the table cursor (%d -> %d) instead of the peek", before, m.tableIdx)
	}
}

// An empty column has no bottom to be at — the note must say what is actually
// true instead of claiming an edge.
func TestCtrlScrollNamesAnEmptyLane(t *testing.T) {
	m := New(memstore.NewWith(board.NewBoard([]*board.Task{{ID: "t-1", Title: "x", Status: "ready", Priority: 100}})), Options{})
	m.Update(tea.WindowSizeMsg{Width: 240, Height: 40})
	m.curLane = m.b.LaneIndex("backlog")
	m.status = ""
	m.Update(ctrlD())
	if !strings.Contains(m.status, "backlog is empty") {
		t.Errorf("scrolling an empty lane must say it is empty, status=%q", m.status)
	}
}

func TestCtrlScrollJumpsTheTableCursorHalfAPage(t *testing.T) {
	m := New(memstore.NewWith(board.NewBoard(scaledTasks(80))), Options{Table: true})
	m.Update(tea.WindowSizeMsg{Width: 240, Height: 40})

	if m.tableIdx != 0 {
		t.Fatalf("fixture: expected the cursor on row 0, got %d", m.tableIdx)
	}
	m.Update(ctrlD())
	if m.tableIdx == 0 {
		t.Fatal("ctrl+d did not move the table cursor")
	}
	half := maxInt(1, maxInt(1, m.h-rowRule-footerH)/2)
	if m.tableIdx != half {
		t.Errorf("ctrl+d moved %d rows, want the half page %d", m.tableIdx, half)
	}
	m.Update(ctrlU())
	if m.tableIdx != 0 {
		t.Errorf("ctrl+u did not come back to row 0, at %d", m.tableIdx)
	}

	// Clamped at the top, and saying so.
	m.status = ""
	m.Update(ctrlU())
	if !strings.Contains(m.status, "already at the top of the table") {
		t.Errorf("a ctrl+u that cannot move must say so, status=%q", m.status)
	}
}

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
	// A column guaranteed to overflow a 30-row terminal.
	m := New(memstore.NewWith(board.NewBoard(scaledTasks(60))), Options{})
	m.Update(tea.WindowSizeMsg{Width: 240, Height: 30})

	lane := m.curLaneName()
	c := m.lay.Col(lane)
	if c == nil || c.Hidden == 0 {
		t.Fatalf("fixture: column %s must overflow (hidden=%v)", lane, c)
	}
	m.Update(ctrlD())
	scrolled := m.lay.Col(lane).Scroll
	if scrolled == 0 {
		t.Fatal("ctrl+d did not scroll the column")
	}
	// The wheel's unit and guards: roughly half the visible cards, never past
	// the point where the column stops hiding anything below.
	m.Update(ctrlU())
	if got := m.lay.Col(lane).Scroll; got >= scrolled {
		t.Errorf("ctrl+u did not scroll back: %d -> %d", scrolled, got)
	}

	// At the very top, ctrl+u must SAY it cannot move.
	for i := 0; i < 50; i++ {
		m.Update(ctrlU())
	}
	m.status = ""
	m.Update(ctrlU())
	if !strings.Contains(m.status, "already at the top") {
		t.Errorf("a ctrl+u that cannot move must say so, status=%q", m.status)
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

package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
)

// A board with no task at all: every key and a whole mouse gesture must
// survive it. Nothing is laid out, so every "first card" and "current lane"
// lookup answers nil, and this is the board view's only net for a handler
// that dereferences one (the boxes view has its own in boxboard_test.go).

func TestAdvEmptyBoardSurvivesEveryGesture(t *testing.T) {
	m := New(&emptyProvider{b: board.NewBoard(nil)}, Options{})
	m.w, m.h = 100, 30
	m.recompute()
	m.relayout()

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("panic on an empty board: %v", r)
		}
	}()

	keys := []string{"j", "k", "h", "l", "enter", "esc", "space", "t", "b", ">", "<",
		"d", "x", "K", "J", "H", "L", "g", "G", "v", "M", "?", "r"}
	for _, k := range keys {
		m.Update(tea.KeyPressMsg{Code: keyCodeFor(k), Text: keyTextFor(k)})
		_ = m.View().Content
	}
	// a mouse gesture over an empty board
	m.Update(tea.MouseClickMsg{X: 5, Y: 7, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: 40, Y: 12, Button: tea.MouseLeft})
	m.Update(tea.MouseReleaseMsg{X: 40, Y: 12, Button: tea.MouseLeft})
	_ = m.View().Content
}

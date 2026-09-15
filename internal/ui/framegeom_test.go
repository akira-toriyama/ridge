package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/store/memstore"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

// Every frame fits its terminal: h rows, each exactly w cells, for every -demo
// state plus the table, the peek with its tree and the day-zoom roadmap, in
// both graph orientations, at every size from the pathological to the design
// range. Below 1x1 the renderer clamps to 1x1 and only liveness is asked.
//
// Two invariants used to live in seven per-view tests and two panic hunts:
// "no wider or taller than the terminal" (the small-size hunts) and "exactly
// the terminal's width on every row" (the CJK shear invariant every view
// carries: one double-width glyph miscounted anywhere shears the column below
// it, and columns composed side by side accumulate the shear one cell per
// column, so it hides at one width and shows at another). One table asks
// both. The design floor is 240 columns (CLAUDE.md); the tiny sizes are
// panic and clamp hunting, not layout policy.
//
// Exactness is asserted from 100 columns up. Below that a layer wider than
// the terminal is clipped by MaxWidth, and a clip that lands on a
// double-width glyph leaves the row one cell short (measured: the slice
// panel's wrapped epic rows at 28 columns render 27) — a fact about
// clipping, not about the layout.
func TestEveryFrameFitsItsTerminal(t *testing.T) {
	sizes := [][2]int{
		{-1, -1}, {0, 0}, {1, 1}, {2, 2}, {3, 3}, {60, 1}, {400, 1}, {1, 100},
		{20, 5}, {20, 8}, {28, 6}, {30, 7}, {40, 10}, {50, 12}, {27, 20}, {28, 20},
		{100, 5}, {100, 6}, {100, 7}, {120, 40}, {140, 24},
		{240, 24}, {240, 50}, {240, 60}, {241, 50}, {259, 50}, {320, 40}, {320, 90},
		{399, 50}, {400, 40}, {400, 50},
	}
	states := append([]string{"", "table", "peektree", "roadmap"}, DemoNames...)
	for _, s := range sizes {
		w, h := s[0], s[1]
		for _, state := range states {
			for _, lr := range []bool{false, true} {
				// Orientation only changes the graph; run the rest once.
				if lr != strings.HasPrefix(state, "graph") && lr {
					continue
				}
				name := fmt.Sprintf("%dx%d/%s", w, h, state)
				if lr {
					name += "/lr"
				}
				t.Run(name, func(t *testing.T) {
					defer func() {
						if r := recover(); r != nil {
							t.Fatalf("panic: %v", r)
						}
					}()
					out, ok := frameFor(t, w, h, state, lr)
					if !ok {
						return
					}
					wantW, wantH := maxInt(w, 1), maxInt(h, 1)
					lines := strings.Split(out, "\n")
					if len(lines) != wantH {
						t.Errorf("%d rows, want %d", len(lines), wantH)
					}
					for i, line := range lines {
						got := lg.Width(line)
						switch {
						case got > wantW:
							t.Errorf("row %d is %d cells, over the terminal's %d: %q", i, got, wantW, line)
						case got < wantW && w >= 100:
							t.Errorf("row %d is %d cells, want exactly %d: %q", i, got, wantW, line)
						}
					}
				})
			}
		}
	}
}

// frameFor renders one state at one size, plain. A -demo that refuses a size
// below the floor (drag needs two cards on screen) is not a finding; at or
// above the floor it is.
func frameFor(t *testing.T, w, h int, state string, lr bool) (string, bool) {
	t.Helper()
	m := New(memstore.New(), Options{GraphLR: lr})
	demo := state
	switch state {
	case "table", "peektree", "roadmap":
		demo = ""
	}
	out, err := m.Dump(w, h, demo, true)
	if err != nil {
		if w >= 240 && h >= 24 {
			t.Fatalf("demo refused a size at or above the floor: %v", err)
		}
		return "", false
	}
	switch state {
	case "table":
		m.view = viewTable
	case "peektree":
		m.peekOpen, m.treeOpen = true, true
		m.syncPeek()
		m.relayout()
	case "roadmap":
		// The day axis has no -demo (it is `-dump -roadmap`); open it the way
		// the roadmapweek demo does, through the real key.
		m.onNormalKey(tea.KeyPressMsg{Code: 'C', Text: "C"})
		if m.view != viewRoadmap {
			t.Fatal("C did not open the roadmap")
		}
	default:
		return out, true
	}
	return ansiStrip(m.View().Content), true
}

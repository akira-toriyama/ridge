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
// Exactness is asserted from 29 columns up. Below that a layer wider than
// the terminal is clipped by MaxWidth, and a clip that lands on a
// double-width glyph leaves the row one cell short — a fact about clipping,
// not about the layout. Measured with no exemption at all: exactly three
// rows in the whole table are short, the filterchips demo's wrapped slice
// rows at 28x20, each 27 cells and each ending on a clipped wide glyph.
//
// The large sizes are few on purpose: every frame here is rendered under
// -race in scripts/check.sh and CI, and a 400-column frame costs about as
// much as all the tiny ones together (measured: 77s under -race for these
// 28 sizes, 94s for 31).
func TestEveryFrameFitsItsTerminal(t *testing.T) {
	sizes := [][2]int{
		{-1, -1}, {0, 0}, {1, 1}, {2, 2}, {3, 3}, {60, 1}, {400, 1}, {1, 100},
		{20, 5}, {20, 8}, {28, 6}, {30, 7}, {40, 10}, {50, 12}, {27, 20}, {28, 20},
		{100, 5}, {100, 7}, {120, 40}, {140, 24},
		{240, 24}, {240, 50}, {241, 50}, {259, 50}, {320, 90}, {399, 50}, {400, 40},
	}
	states := append([]string{"", "table", "peektree", "peekhelp", "roadmap"}, DemoNames...)
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
						case got < wantW && w >= 29:
							t.Errorf("row %d is %d cells, want exactly %d: %q", i, got, wantW, line)
						}
					}
				})
			}
		}
	}
}

// frameFor renders one state at one size, plain. A -demo that refuses a size
// under 120 columns or 24 rows (drag needs two cards on screen; measured,
// every refusal in the table is drag at 100 columns or fewer, or at one row)
// is not a finding; at a size it used to draw, it is.
func frameFor(t *testing.T, w, h int, state string, lr bool) (string, bool) {
	t.Helper()
	m := New(memstore.New(), Options{GraphLR: lr})
	demo := state
	switch state {
	case "table", "peektree", "peekhelp", "roadmap":
		demo = ""
	}
	out, err := m.Dump(w, h, demo, true)
	if err != nil {
		if w >= 120 && h >= 24 {
			t.Fatalf("demo refused a size it used to draw: %v", err)
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
	case "peekhelp":
		// The `?` listing over an open peek: two overlays at once.
		m.peekOpen, m.fullHelp = true, true
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

func advFrameSize(t *testing.T, m *Model) (w, h int) {
	t.Helper()
	out := m.View().Content
	h = lg.Height(out)
	for _, line := range strings.Split(out, "\n") {
		if lw := lg.Width(line); lw > w {
			w = lw
		}
	}
	return w, h
}

// The dragged ghost card is 28 cells wide and ~6 tall and is only clamped to
// max(0, w-28) / max(0, h-cardH). On a terminal narrower/shorter than a card
// that clamp yields 0 and the layer still overflows the canvas.
func TestAdvGhostOverflowsANarrowTerminal(t *testing.T) {
	m := boardModel(t, 24, 14)
	col := m.lay.Col(m.curLaneName())
	if col == nil || len(col.Cards) == 0 {
		t.Fatalf("no card laid out for lane %q at 24x14; the cursor's lane holds no task, "+
			"and this test lifts one", m.curLaneName())
	}
	box := col.Cards[0]
	m.Update(tea.MouseClickMsg{X: box.X + 2, Y: box.Y + 1, Button: tea.MouseLeft})
	m.Update(tea.MouseMotionMsg{X: box.X + 6, Y: box.Y + 3, Button: tea.MouseLeft})
	if !m.drag.moved {
		t.Fatal("drag did not arm")
	}
	gw, gh := advFrameSize(t, m)
	if gw > 24 || gh > 14 {
		t.Errorf("ghost frame is %dx%d, terminal is 24x14", gw, gh)
	}
}

// At h<2 the status and help bars are placed at NEGATIVE y.
func TestAdvChromeIsPlacedAtNegativeY(t *testing.T) {
	m := boardModel(t, 60, 1)
	for _, l := range m.chromeLayers() {
		if l.GetY() < 0 {
			t.Errorf("a chrome layer is placed at y=%d on a 1-row terminal", l.GetY())
		}
	}
}

// peekBox floors its height at 6 rows and anchors it at y=rowColHdr(2) without
// consulting m.h, so on any terminal shorter than 8 rows the panel hangs off
// the bottom of the frame. helpLayer has a MaxWidth/MaxHeight backstop; the
// peek has none.
func TestAdvPeekBoxExceedsTheTerminal(t *testing.T) {
	for _, h := range []int{5, 6, 7} {
		m := boardModel(t, 100, h)
		x, y, w, ph := m.peekBox()
		if y+ph > h {
			t.Errorf("h=%d: peek box y=%d h=%d ends at row %d (terminal has %d); x=%d w=%d",
				h, y, ph, y+ph, h, x, w)
		}
	}
}

func TestAdvPeekLinesFitTheirBox(t *testing.T) {
	for _, size := range [][2]int{{140, 40}, {100, 30}, {80, 24}, {60, 20}} {
		m := boardModel(t, size[0], size[1])
		m.peekOpen, m.treeOpen = true, true
		// pick a task with both directions populated
		for _, task := range m.b.Tasks() {
			if len(task.Deps) > 0 && len(m.g.Blocks(task.ID)) > 0 {
				m.selectID(task.ID, false)
				break
			}
		}
		m.syncPeek()
		_, _, w, _ := m.peekBox()
		inner := maxInt(10, w-4)
		for i, line := range strings.Split(ansiStrip(m.peekContent(inner)), "\n") {
			if lw := lg.Width(line); lw > inner {
				t.Errorf("%dx%d peek line %d is %d cells, box inner width is %d: %q",
					size[0], size[1], i, lw, inner, line)
			}
		}
	}
}

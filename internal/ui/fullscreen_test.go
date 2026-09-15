package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

// The keys every full-screen view answers alike — quit, help, esc, and the
// close pair — exercised in all six.
//
// Written BEFORE those cases were collapsed into fullScreenKey, because three
// of them had no test in any full-screen view: nothing in the suite pressed
// `q` or `ctrl+c` inside one (hardening_test.go's ctrl+c table covers the
// modals only), and every `v` in the suite was on the board or the table. The
// consolidation moves exactly those arms the furthest, so without this the
// refactor could have re-pointed them and shipped green.
//
// The graph is the asymmetry the table exists to record: ⇧space/S RE-ROOTS
// there instead of closing, so its own-key column is empty and the last
// subtest pins the re-root rather than a close.
//
// bite-exempt: this pins behaviour that already existed, deliberately. The
// change it ships with moves those four cases into fullScreenKey and must
// leave every outcome below untouched, so a version of this test that failed
// against the tree before it would be asserting the opposite of what is
// wanted. Measured: the whole file passes against f4eb013 unchanged. What it
// adds is coverage, not a verdict — before it, deleting the Quit arm or the
// View arm outright left the entire repo suite green.
func TestEveryFullScreenViewAnswersTheSharedKeys(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func(*Model)
		kind viewKind
		own  string // the view's own opener, which closes it again; "" for the graph
	}{
		{"graph", func(m *Model) { m.openGraph() }, viewGraph, ""},
		{"map", func(m *Model) { m.openMap("") }, viewMap, "T"},
		{"boxes", func(m *Model) { m.openBoxes() }, viewBoxes, "E"},
		{"roadmap", func(m *Model) { m.openRoadmap() }, viewRoadmap, "C"},
		{"swim", func(m *Model) { m.openSwim() }, viewSwim, "W"},
		{"sweep", func(m *Model) { _ = m.openSweep() }, viewSweep, "X"},
	} {
		open := func(t *testing.T) *Model {
			t.Helper()
			m := boardModel(t, 240, 50)
			tc.open(m)
			if m.view != tc.kind || !m.fullScreen() {
				t.Fatalf("%s did not open (view %v)", tc.name, m.view)
			}
			return m
		}

		t.Run(tc.name+"/esc closes", func(t *testing.T) {
			m := open(t)
			m.onKey(keyMsg("esc"))
			if m.view != viewBoard {
				t.Errorf("esc left the %s open (view %v)", tc.name, m.view)
			}
		})

		t.Run(tc.name+"/v closes", func(t *testing.T) {
			m := open(t)
			m.onKey(keyMsg("v"))
			if m.view != viewBoard {
				t.Errorf("v left the %s open (view %v)", tc.name, m.view)
			}
		})

		t.Run(tc.name+"/? opens help and esc takes it off without closing", func(t *testing.T) {
			m := open(t)
			m.onKey(keyMsg("?"))
			if !m.fullHelp {
				t.Fatalf("? did not open the help overlay in the %s", tc.name)
			}
			if m.view != tc.kind {
				t.Fatalf("? left the %s (view %v)", tc.name, m.view)
			}
			m.onKey(keyMsg("esc"))
			if m.fullHelp {
				t.Errorf("esc did not take the help overlay off in the %s", tc.name)
			}
			if m.view != tc.kind {
				t.Errorf("the esc that closed help also closed the %s", tc.name)
			}
		})

		// Separate from the esc route above, because they are separate arms:
		// esc CLEARS the overlay, `?` TOGGLES it. Nothing in the suite pinned
		// the toggle's off direction — `m.fullHelp = true` in place of the
		// toggle left the whole repo green (measured in review of #117).
		t.Run(tc.name+"/? toggles the help overlay back off", func(t *testing.T) {
			m := open(t)
			m.onKey(keyMsg("?"))
			m.onKey(keyMsg("?"))
			if m.fullHelp {
				t.Errorf("a second ? left the help overlay up in the %s", tc.name)
			}
			if m.view != tc.kind {
				t.Errorf("toggling help closed the %s", tc.name)
			}
		})

		for _, q := range []struct {
			label string
			msg   tea.KeyPressMsg
		}{{"q", keyMsg("q")}, {"ctrl+c", ctrlC()}} {
			t.Run(tc.name+"/"+q.label+" quits", func(t *testing.T) {
				m := open(t)
				c := m.onKey(q.msg)
				if c == nil {
					t.Fatalf("%s in the %s produced no Cmd", q.label, tc.name)
				}
				if _, ok := c().(tea.QuitMsg); !ok {
					t.Errorf("%s in the %s produced %T, want quit", q.label, tc.name, c())
				}
				if m.view != tc.kind {
					t.Errorf("%s left the %s before quitting", q.label, tc.name)
				}
			})
		}

		if tc.own == "" {
			continue
		}
		t.Run(tc.name+"/"+tc.own+" closes", func(t *testing.T) {
			m := open(t)
			m.onKey(keyMsg(tc.own))
			if m.view != viewBoard {
				t.Errorf("%s left the %s open (view %v)", tc.own, tc.name, m.view)
			}
		})
	}

	// The graph's half of the asymmetry: keys.Graph is bound in the graph and
	// must re-root, not close. It is pinned here because it is the behaviour
	// that makes the graph's empty own-key correct — NOT as a guard on
	// fullScreenKey's argument. onGraphKey answers keys.Graph in a case of its
	// own above the default arm, so handing the helper m.keys.Graph is a no-op
	// and this subtest stays green through it (measured in review of #117).
	t.Run("graph/S re-roots instead of closing", func(t *testing.T) {
		m := boardModel(t, 240, 50)
		m.openGraph()
		m.onKey(keyMsg("S"))
		if m.view != viewGraph {
			t.Errorf("S closed the graph; it must re-root (view %v)", m.view)
		}
	})
}

// packBands's geometry, which nothing pinned before. Measured 2026-09-15:
// deleting the inter-column gap left the ENTIRE internal/ui suite green,
// including the two column-width tests the task that filed this named as its
// safety net — they assert on the block renderers, which run one level down.
//
// bite-exempt: it pins behaviour that already existed. mapBands and boxBands
// composed bands exactly this way before they were merged into packBands; all
// 236 -demo frames are byte-identical across the change.
func TestPackedBandsKeepTheGridWidthExact(t *testing.T) {
	const cols, colW, h, gap = 3, 4, 3, 2
	blocks := []placedBlock{
		{Col: 0, Y: 0, Lines: []string{"aaaa", "bbbb"}},
		{Col: 2, Y: 1, Lines: []string{"cccc"}},
		// Starts on the last row and runs two lines past the canvas.
		{Col: 1, Y: 2, Lines: []string{"dddd", "eeee", "ffff"}},
	}
	bands := packBands(blocks, cols, colW, h, gap)

	if len(bands) != h {
		t.Fatalf("packBands returned %d bands, want %d", len(bands), h)
	}
	// Row 1 is the one with a block in the LAST column, so nothing is trimmed
	// off its end and the full grid width is measurable. This is the assertion
	// the deleted gap has to fail.
	if w, want := lg.Width(bands[1]), cols*colW+(cols-1)*gap; w != want {
		t.Errorf("a band reaching the last column measures %d cells, want %d "+
			"(%d columns of %d, %d gaps of %d)", w, want, cols, colW, cols-1, gap)
	}
	// 4 cells of block, then gap + blank column + gap, then 4 more.
	if want := "bbbb" + strings.Repeat(" ", gap+colW+gap) + "cccc"; bands[1] != want {
		t.Errorf("band 1 = %q, want %q", bands[1], want)
	}
	// Trailing blank columns are trimmed; fillCanvas re-pads them.
	if bands[0] != "aaaa" {
		t.Errorf("band 0 = %q, want the trailing blank columns trimmed to %q", bands[0], "aaaa")
	}
	// The over-long block contributed its first line and no more.
	if bands[2] != "      dddd" {
		t.Errorf("band 2 = %q, want the clipped block's first line only", bands[2])
	}

	// The same join in Japanese. It catches nothing the ASCII case does not —
	// packBands places lines and never measures them — but the grid is composed
	// in DISPLAY cells, and this is the worked example that says a two-cell
	// glyph does not shear the column to its right.
	t.Run("the same grid in Japanese", func(t *testing.T) {
		const colW, gap = 8, 2
		// The LAST column must fill its width exactly: TrimRight strips a
		// padded tail, so a band ending in pad()'s spaces is legitimately
		// short. Four Japanese glyphs are eight cells, colW with nothing over.
		first, last := pad("常備菜", colW), "九州旅行"
		for _, s := range []string{first, last} {
			if lg.Width(s) != colW {
				t.Fatalf("setup: %q measures %d cells, want %d", s, lg.Width(s), colW)
			}
		}
		bands := packBands([]placedBlock{
			{Col: 0, Y: 0, Lines: []string{first}},
			{Col: 1, Y: 0, Lines: []string{last}},
		}, 2, colW, 1, gap)
		if w, want := lg.Width(bands[0]), 2*colW+gap; w != want {
			t.Errorf("a Japanese band measures %d cells, want %d", w, want)
		}
	})
}

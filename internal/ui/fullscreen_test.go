package ui

import (
	"testing"

	tea "charm.land/bubbletea/v2"
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

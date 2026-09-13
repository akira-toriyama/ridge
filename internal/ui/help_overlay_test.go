package ui

import (
	"strings"
	"testing"
)

// The `?` listing is drawn OVER a modal while the modal holds the keyboard,
// and `?` is not bound inside every modal — so an overlay left standing when
// one opened could not always be taken back off. `? E m` from the box board
// was the reachable spelling: the epic overlay had the keyboard while the
// frame showed only the help listing, and a blind ⏎ then acted on a box the
// user could not see.
func TestOpeningAModalClosesTheHelpOverlay(t *testing.T) {
	const w, h = 240, 50
	cases := []struct {
		name string
		keys []string
		want mode
	}{
		{"box board epic edit", []string{"?", "E", "m"}, modeEpic},
		{"add", []string{"?", "a"}, modeAdd},
		{"note", []string{"?", "n"}, modeEdit},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := boardModel(t, w, h)
			press(m, tc.keys...)
			if m.mode != tc.want {
				t.Fatalf("%s reached mode %v, want %v — the keys no longer open that modal",
					strings.Join(tc.keys, " "), m.mode, tc.want)
			}
			if m.fullHelp {
				t.Errorf("%s left the help overlay up over the %v modal",
					strings.Join(tc.keys, " "), tc.want)
			}
		})
	}
}

// The `?` listing is the whole frame while it is up. Two surfaces still acted
// under it because `?` IS bound in them and only their esc branch looked:
// `⏎ ? J ⏎` moved a card to the bottom of another lane, and `X ? ⏎ ⏎` armed
// and applied a bulk archive — both with nothing but the listing on screen.
// The first key takes the overlay off and does nothing else.
func TestNoMutationRunsUnderTheHelpOverlay(t *testing.T) {
	const w, h = 240, 40

	t.Run("move mode commit", func(t *testing.T) {
		m := boardModel(t, w, h)
		id := m.curTask().ID
		before := laneOf(m, id)
		press(m, "enter")
		if m.mode != modeMove {
			t.Fatalf("⏎ reached mode %v, want move", m.mode)
		}
		press(m, "?")
		if !m.fullHelp {
			t.Fatal("? did not open the listing in move mode")
		}
		press(m, "J", "enter")
		if got := laneOf(m, id); got != before {
			t.Errorf("%s moved from %s to %s while the frame showed only the help listing",
				id, before, got)
		}
		if m.fullHelp {
			t.Error("the first key under the listing did not take it off")
		}
	})

	t.Run("sweep gate", func(t *testing.T) {
		m := boardModel(t, w, h)
		before := len(m.b.Tasks())
		press(m, "X")
		if m.view != viewSweep {
			t.Fatalf("X reached view %v, want the sweep view", m.view)
		}
		press(m, "?")
		if !m.fullHelp {
			t.Fatal("? did not open the listing in the sweep view")
		}
		press(m, "enter", "enter")
		if got := len(m.b.Tasks()); got != before {
			t.Errorf("the board went from %d tasks to %d while the frame showed only the help listing",
				before, got)
		}
		if m.fullHelp {
			t.Error("the first key under the listing did not take it off")
		}
	})
}

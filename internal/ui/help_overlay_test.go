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

// The same state, asserted on the FRAME rather than the flag: whatever has
// the keyboard has to be what is drawn.
func TestTheEpicOverlayIsVisibleWhenItHasTheKeyboard(t *testing.T) {
	m := boardModel(t, 240, 50)
	press(m, "?", "E", "m")
	if m.mode != modeEpic {
		t.Fatalf("? E m reached mode %v, want the epic overlay", m.mode)
	}
	frame := ansiStrip(m.View().Content)
	if strings.Contains(frame, "quit · ? help") && !strings.Contains(frame, "box") {
		t.Errorf("the epic overlay holds the keyboard but the frame is the help listing:\n%s",
			firstLines(frame, 4))
	}
}

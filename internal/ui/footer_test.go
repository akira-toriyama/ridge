package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"

	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// dumpFrame renders one plain frame in the given transient state.
func dumpFrame(t *testing.T, w, h int, demo string) []string {
	t.Helper()
	m := New(memstore.New(), Options{})
	out, err := m.Dump(w, h, demo, true)
	if err != nil {
		t.Fatalf("dump %q: %v", demo, err)
	}
	return strings.Split(out, "\n")
}

// The frame carries no key list anywhere except the `?` overlay. A partial one
// is worse than none: the old footer listed `>` and not `<`, and a reader took
// that for the whole surface and concluded the jump was one-way.
//
// The fingerprint is the WithHelp text of bindings only a footer ever printed.
// " • ", bubbles' ShortHelpView separator, looked like the tighter check but is
// not ours to claim: task bodies are markdown and one of the fixture's uses
// bulleted lists, so the peek renders " • " as content.
func TestNoKeyListOutsideTheHelpOverlay(t *testing.T) {
	// Not "move mode" or "commit": the status line legitimately names the exit
	// from the mode you just entered, which is a message, not a key list.
	footerOnly := []string{"jump to blocker", "mouse on/off", "board/table", "only blocked"}

	for _, demo := range []string{"", "move", "drag", "add", "edit", "graph", "slice", "sort"} {
		name := demo
		if name == "" {
			name = "board"
		}
		t.Run(name, func(t *testing.T) {
			frame := strings.Join(dumpFrame(t, 240, 50, demo), "\n")
			for _, hint := range footerOnly {
				if strings.Contains(frame, hint) {
					t.Errorf("key list leaked into the frame: found %q outside the ? overlay", hint)
				}
			}
		})
	}

	t.Run("help overlay still lists them", func(t *testing.T) {
		frame := strings.Join(dumpFrame(t, 240, 50, "help"), "\n")
		for _, want := range []string{"jump to blocker", "jump back", "dep graph", "slice panel"} {
			if !strings.Contains(frame, want) {
				t.Errorf("? overlay lost %q — it is the only place the keys are listed now", want)
			}
		}
	})
}

// The last row is the status line, and it is reserved even when there is
// nothing to say, so a message appearing never shifts the board under the
// cursor.
func TestLastRowIsTheStatusLine(t *testing.T) {
	lines := dumpFrame(t, 240, 50, "")
	if len(lines) != 50 {
		t.Fatalf("frame is %d rows, want 50", len(lines))
	}
	last := strings.TrimSpace(lines[len(lines)-1])
	if last != "fixture · 23 tasks" {
		t.Errorf("last row = %q, want the startup status line", last)
	}

	// And with the status cleared it is blank rather than absent.
	m := New(memstore.New(), Options{})
	m.status = ""
	out, err := m.Dump(240, 50, "", true)
	if err != nil {
		t.Fatal(err)
	}
	blank := strings.Split(out, "\n")
	if len(blank) != 50 {
		t.Fatalf("frame with an empty status is %d rows, want 50", len(blank))
	}
	if strings.TrimSpace(blank[len(blank)-1]) != "" {
		t.Errorf("last row = %q, want blank", blank[len(blank)-1])
	}
	// The board must not have grown into the reserved row.
	if blank[boardTop] != lines[boardTop] {
		t.Error("clearing the status moved the board")
	}
}

// `? help` is the whole in-app pointer to the key surface, so it must be on
// screen in every view and never scroll away.
func TestTitleRowPointsAtTheHelpOverlay(t *testing.T) {
	for _, demo := range []string{"", "graph", "slice", "sort"} {
		name := demo
		if name == "" {
			name = "board"
		}
		t.Run(name, func(t *testing.T) {
			lines := dumpFrame(t, 240, 50, demo)
			if !strings.Contains(lines[rowTitle], "? help") {
				t.Errorf("title row lost the help pointer: %q", strings.TrimSpace(lines[rowTitle]))
			}
		})
	}
}

// Dropping a row must not have cost the frame its rectangularity at any of the
// widths this board is read at.
func TestFrameStaysRectangularAfterTheFooterWent(t *testing.T) {
	for _, w := range []int{240, 241, 259, 320, 399, 400} {
		for _, demo := range []string{"", "graph", "edit"} {
			lines := dumpFrame(t, w, 50, demo)
			for i, line := range lines {
				if got := lipgloss.Width(line); got != w {
					t.Errorf("w=%d demo=%q row %d is %d cells wide", w, demo, i, got)
				}
			}
		}
	}
}

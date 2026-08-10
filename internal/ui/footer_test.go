package ui

import (
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/akira-toriyama/ridge/internal/board"
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
// The fingerprint is bubbles' ShortHelpView separator, " • ", counted PER LINE.
//
// Two weaker fingerprints were tried and both were wrong, in opposite
// directions. A bare Contains(" • ") is too loose: task bodies are markdown and
// the fixture has bulleted lists, so the peek renders single bullets as
// content. A list of WithHelp texts was far too tight — of the four strings it
// used, only "jump to blocker" was ever printed by a footer at all, so the
// move, edit and graph subtests passed against `main` with all four footers
// present (independent review of PR #21, blocker 2).
//
// Two or more separators on ONE line is the discriminator: every deleted footer
// listed at least six bindings, and no markdown line in the fixture carries two
// bullets.
func TestNoKeyListOutsideTheHelpOverlay(t *testing.T) {
	for _, demo := range []string{"", "move", "drag", "add", "edit", "graph", "slice", "sort"} {
		name := demo
		if name == "" {
			name = "board"
		}
		t.Run(name, func(t *testing.T) {
			for _, line := range dumpFrame(t, 240, 50, demo) {
				if strings.Count(line, " • ") >= 2 {
					t.Errorf("key list leaked into the frame: %q", strings.TrimSpace(line))
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

// Every view that prints `? help` must actually draw the overlay when `?` is
// pressed. The graph did not: it composes its frame as a string rather than
// through the compositor, so `?` set the flag and changed nothing on screen —
// which is the exact failure the footers were deleted for, one layer up.
func TestHelpOverlayOpensInEveryViewThatAdvertisesIt(t *testing.T) {
	for _, tc := range []struct{ name, demo string }{
		{"board", ""},
		{"table", "sort"},
		{"graph", "graph"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(memstore.New(), Options{})
			if _, err := m.Dump(240, 50, tc.demo, true); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(strings.Join(m.frameRows(t, 240, 50), "\n"), "? help") {
				t.Fatal("setup: this view does not advertise ? help")
			}
			m.Update(keyMsg("?"))
			if !m.fullHelp {
				t.Fatal("? did not set fullHelp")
			}
			frame := strings.Join(m.frameRows(t, 240, 50), "\n")
			if !strings.Contains(frame, "jump to blocker") {
				t.Error("? is advertised here but the overlay is never drawn")
			}
		})
	}
}

// frameRows renders the model as it currently stands, without resetting state.
func (m *Model) frameRows(t *testing.T, w, h int) []string {
	t.Helper()
	m.w, m.h = w, h
	m.help.SetWidth(w)
	m.recompute()
	m.relayout()
	var s string
	switch m.view {
	case viewTable:
		s = m.renderTable()
	case viewGraph:
		s = m.renderGraph()
	default:
		s = m.renderBoard()
	}
	return strings.Split(ansi.Strip(s), "\n")
}

// A live store behind the schema gate keeps its read-only warning in the
// opening frame. newModel sets that warning once and nothing ever restores it,
// so whatever the constructor writes over it is gone for the whole session —
// and the startup note added here first overwrote it with "fixture · N tasks",
// which is worse than a lost warning: "fixture" is the one word that means
// nothing you do touches disk (independent review of PR #21, blocker 1).
func TestReadOnlyBoardKeepsItsWarningInTheOpeningFrame(t *testing.T) {
	ro := memstore.New().Board()
	gated := board.NewStoreBoard(ro.Lanes(), ro.Tasks(), ro.Epics(), false, "board-behind")
	p := newScriptedProvider(func() *board.Board { return gated }) // Live() is true

	m := New(p, Options{})
	if !strings.Contains(m.status, "read-only") {
		t.Errorf("status = %q, want the read-only warning", m.status)
	}
	if !m.statusErr {
		t.Error("the read-only warning lost its error styling")
	}
	if strings.Contains(m.status, "fixture") {
		t.Errorf("a live gated store was labelled a fixture: %q", m.status)
	}

	out, err := m.Dump(240, 50, "", true)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(out, "\n")
	if last := lines[len(lines)-1]; !strings.Contains(last, "read-only") {
		t.Errorf("last row = %q, want the read-only warning", strings.TrimSpace(last))
	}
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

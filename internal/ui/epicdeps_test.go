package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// The peek resolves the selected task's epic deps — furrow's "open after
// those close" edge, in `furrow epic dep --list`'s own "waits on" wording.
// t-y4st's box (e-c4mt) waits on the open e-fw2m and carries two deps furrow
// has already resolved away (outside open_deps): e-2b7h, which the --all read
// shows CLOSED, and e-x0k9, which it cannot resolve at all. Those are furrow's
// own three states — `epic dep --list` prints [open]/[closed]/[?] — and the
// line must keep them apart. Collapsing them is the failure this pins, and
// calling the third one "satisfied" is the specific collapse that puts a
// reassuring word on what furrow lints as an error.
func TestPeekResolvesTheEpicsOwnDeps(t *testing.T) {
	m := boardModel(t, 240, 50)
	if !m.selectID("t-y4st", false) {
		t.Fatal("t-y4st is not on the fixture board")
	}
	press(m, "space")
	out := frame(m)

	if !strings.Contains(out, "epic waits on") {
		t.Fatal("the peek must carry the epic's dep line")
	}
	if !strings.Contains(out, "e-fw2m (6/18) 九州") {
		t.Error("an OPEN dep must resolve to id, progress, title — progress first, so a truncated CJK title cannot eat it")
	}
	if !strings.Contains(out, "e-2b7h (0/0)") || !strings.Contains(out, "(closed)") {
		t.Error("a resolved-away dep the board shows CLOSED resolves like any other and says so")
	}
	if !strings.Contains(out, "e-x0k9 (missing)") {
		t.Error("a dep that resolves to nothing is furrow's epic-dep-missing — never a reassuring word")
	}
}

// The line exists only when the box still WAITS — the same open_deps gate the
// slice panel's →N counts, so the two surfaces cannot contradict each other.
// A dep-less epic, an unfiled task, and (the case the fixture cannot hold) an
// epic whose every dep is already satisfied all stay without it: furrow's
// revisit calls that state epic_dep_done — its turn to open, not a wait.
func TestPeekOmitsTheDepLineWhenNothingIsOpen(t *testing.T) {
	for _, id := range []string{"t-kv82", "t-ehk7"} { // e-p3dx member; unfiled
		m := boardModel(t, 240, 50)
		if !m.selectID(id, false) {
			t.Fatalf("%s is not on the fixture board", id)
		}
		press(m, "space")
		if strings.Contains(frame(m), "waits on") {
			t.Errorf("%s: no epic dep to report, so no line", id)
		}
	}

	b := board.NewBoard(
		[]*board.Task{{ID: "t-solo", Title: "箱の中の一枚", Status: "backlog", Priority: 10, Epic: "e-wait"}},
		board.EpicInfo{ID: "e-wait", Title: "全部満了した箱", Total: 1,
			Deps: []string{"e-gone1", "e-gone2"}}, // open_deps empty: all satisfied
	)
	m := New(memstore.NewWith(b), Options{})
	m.Update(tea.WindowSizeMsg{Width: 240, Height: 50})
	if !m.selectID("t-solo", false) {
		t.Fatal("t-solo is not on the synthetic board")
	}
	press(m, "space")
	out := frame(m)
	if strings.Contains(out, "waits on") {
		t.Error("every dep is satisfied — the box waits on nothing, and the panel shows no arrow either")
	}
}

// The two dep renderings the fixture cannot hold: an open dep that is STUCK
// (marked, warn-coloured like the own-epic line's STUCK) and a dep furrow
// reports open but this read cannot resolve — shown as the bare id, the
// promise the branch's comment makes. Unreachable against real furrow (the
// unscoped read serves every open epic), so a synthetic board is the only
// harness.
func TestPeekMarksStuckAndUnresolvableOpenDeps(t *testing.T) {
	b := board.NewBoard(
		[]*board.Task{{ID: "t-solo", Title: "箱の中の一枚", Status: "backlog", Priority: 10, Epic: "e-wait"}},
		board.EpicInfo{ID: "e-wait", Title: "待つ箱", Total: 1,
			Deps: []string{"e-stuck", "e-ghost"}, OpenDeps: []string{"e-stuck", "e-ghost"}},
		board.EpicInfo{ID: "e-stuck", Title: "詰まった箱", Done: 1, Total: 4, Stuck: true},
	)
	m := New(memstore.NewWith(b), Options{})
	m.Update(tea.WindowSizeMsg{Width: 240, Height: 50})
	if !m.selectID("t-solo", false) {
		t.Fatal("t-solo is not on the synthetic board")
	}
	press(m, "space")
	out := frame(m)
	if !strings.Contains(out, "e-stuck (1/4) STUCK 詰まった箱") {
		t.Error("an open stuck dep must carry the STUCK marker between progress and title")
	}
	if !strings.Contains(out, "e-ghost") || strings.Contains(out, "e-ghost (") {
		t.Error("an open-but-unresolvable dep is the bare id — no invented state")
	}
}

// The `-demo epicdeps` frame must actually carry the line it exists to show.
// TestEveryAdvertisedDemoRenders only proves the frame differs from the bare
// board, and the selectID alone satisfies that — a demo whose peek never
// opened stayed green until this pinned the content.
func TestEpicDepsDemoCarriesTheDepLine(t *testing.T) {
	m := New(memstore.New(), Options{})
	out, err := m.Dump(240, 50, "epicdeps", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"epic waits on", "e-fw2m (6/18)", "(closed)", "e-x0k9 (missing)"} {
		if !strings.Contains(out, want) {
			t.Errorf("-demo epicdeps: %q is missing from the frame", want)
		}
	}
}

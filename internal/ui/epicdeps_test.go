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
// t-y4st's box (e-c4mt) waits on the open e-fw2m and carries e-2b7h, a dep
// furrow has already resolved away (outside open_deps). The satisfied one
// says "(satisfied)", never "(closed)": the open-only read cannot tell a
// closed dep from a dangling one, so the line must not claim to.
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
	if !strings.Contains(out, "e-2b7h (satisfied)") {
		t.Error("a dep outside open_deps is satisfied — say that, and nothing more specific")
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
	for _, want := range []string{"epic waits on", "e-fw2m (6/18)", "e-2b7h (satisfied)"} {
		if !strings.Contains(out, want) {
			t.Errorf("-demo epicdeps: %q is missing from the frame", want)
		}
	}
}

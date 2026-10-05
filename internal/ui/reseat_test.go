package ui

import (
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
)

// A re-read that takes the row under a full-screen cursor lands the cursor
// on the nearest surviving row and says so; the view's clamp alone put it
// on the first row in silence, and the next ⏎ acted there (t-v8j6).
func TestAReReadThatDropsTheSelectedRowSaysWhereTheCursorWent(t *testing.T) {
	done := func(id string) func(*testing.T, *driftStore) {
		return func(t *testing.T, d *driftStore) {
			d.onReload = func(b *board.Board) {
				if _, err := b.MoveTo(id, b.DoneLane(), 0); err != nil {
					t.Fatal(err)
				}
			}
		}
	}
	for _, tc := range []struct {
		name string
		open func(*Model)
		sel  func(*Model) string
		what string
	}{
		{"roadmap", func(m *Model) { m.openRoadmap() }, func(m *Model) string { return m.road.sel }, "roadmap"},
		{"map", func(m *Model) { m.openMap("") }, func(m *Model) string { return m.depmap.sel }, "dep map"},
		{"swim", func(m *Model) { m.openSwim() }, func(m *Model) string { return m.swim.lay.IDOf(m.swim.sel) }, "swimlane"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDriftStore()
			m := New(d, Options{})
			m.w, m.h, m.sized = 240, 50, true
			m.recompute()
			m.relayout()
			tc.open(m)
			frame(m)
			// Off the first row, so "the first row" and "the neighbour" differ.
			press(m, "down", "down")
			frame(m)
			gone := tc.sel(m)
			if gone == "" {
				t.Skip("the walk ended on a row that names no task")
			}
			done(gone)(t, d)
			c := m.onKey(keyMsg("r"))
			if c == nil {
				t.Fatal("r must re-read")
			}
			runCmd(m, c)
			frame(m)
			now := tc.sel(m)
			if now == gone || now == "" {
				t.Fatalf("the cursor must leave %s for a row that exists, got %q", gone, now)
			}
			want := gone + " left the " + tc.what + " — the cursor is on " + now
			if !strings.Contains(m.status, "reloaded") || !strings.Contains(m.status, want) {
				t.Errorf("status = %q, want the re-read and %q", m.status, want)
			}
			press(m, "esc")
			if m.cursorID() != now || !strings.Contains(m.status, "the cursor followed") {
				t.Errorf("esc must carry the landing to the board and say so: cursor %q status %q", m.cursorID(), m.status)
			}
		})
	}
}

// A close note claims "the cursor followed" only when it did: an unmoved
// cursor is not carried, and the note once said it was.
func TestCloseNoteClaimsFollowedOnlyWhenTheCursorCame(t *testing.T) {
	m := boardModel(t, 240, 50)
	before := m.cursorID()
	m.openRoadmap()
	frame(m)
	press(m, "esc")
	if m.cursorID() != before || m.status != "board view" {
		t.Errorf("an unwalked roadmap closes to %q with %q", m.cursorID(), m.status)
	}
	m.openRoadmap()
	frame(m)
	press(m, "down")
	sel := m.road.sel
	press(m, "esc")
	if m.cursorID() != sel || !strings.Contains(m.status, "the cursor followed the roadmap") {
		t.Errorf("a walked roadmap closes to %q with %q, want %q", m.cursorID(), m.status, sel)
	}
}

// A graph whose root left the board closes: it stayed "rooted on" the
// missing id, one unresolved node answering ⏎ with "already the root".
func TestAReReadThatDropsTheGraphRootClosesTheGraph(t *testing.T) {
	d := newDriftStore()
	m := New(d, Options{})
	m.w, m.h, m.sized = 240, 50, true
	m.recompute()
	m.relayout()
	if !m.selectID("t-9sa6", false) {
		t.Fatal("setup: select")
	}
	m.openGraph()
	frame(m)
	if m.view != viewGraph {
		t.Fatal("setup: the graph must open")
	}
	if _, err := d.Remove([]string{"t-9sa6"}, board.RemoveOptions{Force: true, Apply: true}); err != nil {
		t.Fatal(err)
	}
	c := m.onKey(keyMsg("r"))
	runCmd(m, c)
	if m.view != viewBoard || !strings.Contains(m.status, "the graph's root t-9sa6 left the board — the graph closed") {
		t.Errorf("view %v status %q", m.view, m.status)
	}
	if len(m.pinned) != 0 {
		t.Errorf("closing must not pin the lost id: %v", m.pinned)
	}
}

// A landing is not a walk: when the lost row was the view's opening
// fallback — the board cursor's task is not in the view — closing must
// leave the board cursor where the user left it, and say only where they are.
func TestALandingDoesNotCarryTheBoardCursor(t *testing.T) {
	d := newDriftStore()
	m := New(d, Options{})
	m.w, m.h, m.sized = 240, 50, true
	m.recompute()
	m.relayout()
	before := m.cursorID()
	m.openRoadmap()
	frame(m)
	gone := m.road.sel
	d.onReload = func(b *board.Board) {
		if _, err := b.MoveTo(gone, b.DoneLane(), 0); err != nil {
			t.Fatal(err)
		}
	}
	runCmd(m, m.onKey(keyMsg("r")))
	frame(m)
	if m.road.sel == gone || m.road.moved {
		t.Fatalf("the cursor must land without becoming a walk: sel %q moved %v", m.road.sel, m.road.moved)
	}
	press(m, "esc")
	if before != gone && m.cursorID() != before {
		t.Errorf("the board cursor moved from %q to %q", before, m.cursorID())
	}
	if m.status != "board view" {
		t.Errorf("status = %q", m.status)
	}
}

// The landing is the surviving row fewest steps away, the forward one on a
// tie, and an unlabelled re-read (a write's reconcile) appends the note to
// the gesture's own line.
func TestTheLandingIsTheNearestSurvivingRow(t *testing.T) {
	d := newDriftStore()
	m := New(d, Options{})
	m.w, m.h, m.sized = 240, 50, true
	m.recompute()
	m.relayout()
	m.openRoadmap()
	frame(m)
	rows := m.road.lay.Rows
	if len(rows) < 5 {
		t.Fatalf("setup: %d dated rows", len(rows))
	}
	m.road.sel = rows[2].ID
	d.onReload = func(b *board.Board) {
		for _, id := range []string{rows[2].ID, rows[3].ID} {
			if _, err := b.MoveTo(id, b.DoneLane(), 0); err != nil {
				t.Fatal(err)
			}
		}
	}
	m.note("the gesture's own line")
	runCmd(m, m.reloadCmd(""))
	if m.road.sel != rows[1].ID {
		t.Errorf("landed on %q, want the row one step back %q (two forward is %q)", m.road.sel, rows[1].ID, rows[4].ID)
	}
	want := "the gesture's own line · " + rows[2].ID + " left the roadmap — the cursor is on " + rows[1].ID
	if m.status != want {
		t.Errorf("status = %q, want %q", m.status, want)
	}
}

// The close note of a view whose cursor was not walked: where the user is,
// and no claim. (The graph opens on the board cursor's own task, so its
// selection and the board cursor agree without a walk.)
func TestUnwalkedViewsCloseWithoutClaimingTheCursorFollowed(t *testing.T) {
	for _, tc := range []struct {
		name string
		open func(*Model)
	}{
		{"map", func(m *Model) { m.openMap("") }},
		{"swim", func(m *Model) { m.openSwim() }},
	} {
		m := boardModel(t, 240, 50)
		tc.open(m)
		frame(m)
		press(m, "esc")
		if m.view != viewBoard || strings.Contains(m.status, "followed") {
			t.Errorf("%s: view %v status %q", tc.name, m.view, m.status)
		}
	}
}

// A graph's selection on an unresolved dep — a node with no task behind
// it — survives a re-read that changed nothing.
func TestAReReadKeepsAGraphSelectionTheLayoutStillHas(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.openGraph()
	frame(m)
	for _, n := range m.graph.lay.Real() {
		if n.ID != m.graph.focus {
			m.graph.sel = n.ID
			break
		}
	}
	sel := m.graph.sel
	seat := m.takeSeat()
	m.recompute()
	if note := m.reseat(seat); note != "" || m.graph.sel != sel {
		t.Errorf("note %q sel %q, want the selection kept on %q", note, m.graph.sel, sel)
	}
}

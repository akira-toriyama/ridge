package ui

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// allDepStatesBoard carries one box whose deps cover all six states at once —
// open, open-and-stuck, open-but-unresolvable, closed, missing, satisfied. No
// board furrow produces holds the last one (epicfacts.go says why), and the
// fixture reaches only three of the six across its four dep edges, so the
// states are built rather than found.
func allDepStatesBoard(t *testing.T, w int) *Model {
	t.Helper()
	b := board.NewBoard(
		[]*board.Task{{ID: "t-solo", Title: "箱の中の一枚", Status: "backlog", Priority: 10, Epic: "e-wait"}},
		board.EpicInfo{
			ID: "e-wait", Title: "待つ箱", Done: 2, Total: 9,
			Active: true, Standing: true, Pinned: true, Stuck: true,
			Repos: []string{"tomo/a", "tomo/b"}, Labels: []string{"refactor", "bug"},
			Meta:     map[string]string{"k2": "v", "k1": "v"},
			Deps:     []string{"e-open", "e-stuck", "e-ghost", "e-closed", "e-miss", "e-sat"},
			OpenDeps: []string{"e-open", "e-stuck", "e-ghost"},
		},
		board.EpicInfo{ID: "e-open", Title: "開いている箱", Done: 1, Total: 5},
		board.EpicInfo{ID: "e-stuck", Title: "詰まった箱", Done: 1, Total: 4, Stuck: true},
		board.EpicInfo{ID: "e-closed", Title: "閉じた箱", Done: 3, Total: 3,
			Closed: time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)},
		board.EpicInfo{ID: "e-sat", Title: "満了した箱", Done: 2, Total: 6},
	)
	m := New(memstore.NewWith(b), Options{})
	m.Update(tea.WindowSizeMsg{Width: w, Height: 50})
	return m
}

// The three surfaces that state an epic's dep edges, over every state at once.
// They share one classifier and deliberately do NOT share their wording: the
// glyph prefix, the STUCK marker and whether a satisfied dep shows its numbers
// are per-surface choices. Written out in full because the invariant was held
// by prose alone before — three comments forbidding a second vocabulary, over
// three ladders that had already drifted to six, five and four branches.
//
// bite-exempt: it pins behaviour that already existed. All three ladders
// rendered exactly this before they were re-expressed over epicDepStateOf;
// measured, 80 surface renders across six states and ten chip sets are
// byte-identical across the change.
func TestTheThreeEpicDepLaddersAgreeWhereTheyMustAndDifferWhereTheyShould(t *testing.T) {
	for _, tc := range []struct {
		surface string
		open    func(*Model)
		want    []string
		// want alone cannot separate the two unresolvable states: "e-ghost" is
		// a prefix of "e-ghost (missing)", so a surface that labelled an OPEN
		// unresolvable dep as missing passed (found in review of #120). These
		// are the collapses each surface must not make.
		notWant []string
	}{
		{
			// The peek is the only surface that marks a dep's STUCK, and the
			// only one that distinguishes all six states.
			surface: "peek",
			open:    func(m *Model) { m.selectID("t-solo", false); press(m, "space") },
			want: []string{
				"epic waits on",
				"e-open (1/5) 開いている箱",
				"e-stuck (1/4) STUCK 詰まった箱",
				"e-ghost",
				"e-closed (3/3) 閉じた箱 (closed)",
				"e-miss (missing)",
				"e-sat (satisfied)",
			},
			notWant: []string{
				"e-ghost (missing)",   // open, merely unresolvable — never furrow's [?]
				"e-ghost (satisfied)", // and never the reassuring word either
				"e-open (1/5) STUCK",  // only e-stuck is stuck
			},
		},
		{
			// The box overview collapses stuck into open, and shows a
			// satisfied dep as the bare id.
			surface: "box overview strip",
			open: func(m *Model) {
				m.boxesAll = true
				m.openBoxes()
				for _, r := range m.buildBoxes().Rows {
					if r.ID == "e-wait" {
						m.boxesSel = r.Key
					}
				}
			},
			want: []string{
				"waits on",
				"e-open (1/5) 開いている箱",
				"e-stuck (1/4) 詰まった箱", // no STUCK marker here
				"e-ghost",
				"e-closed (3/3) 閉じた箱 (closed)",
				"e-miss (missing)",
				"e-sat (satisfied)",
			},
			notWant: []string{
				"e-ghost (missing)",
				"e-stuck (1/4) STUCK", // the strip marks no dep's stuck state
				"e-sat (2/6)",         // and shows a satisfied dep as the bare id
			},
		},
		{
			// The overlay's list is the editing surface, so every row is a
			// handle and a satisfied dep keeps its numbers. One glyph for
			// every open state.
			surface: "epic overlay deps list",
			open: func(m *Model) {
				m.toggleSlice()
				m.sliceField = sliceEpic
				m.sliceEpicAll = true
				for i, r := range m.sliceRows() {
					if r.value == "e-wait" {
						m.sliceIdx = i
					}
				}
				press(m, "m")
				if m.epic == nil {
					return
				}
				m.epic.menuIdx = int(epicFieldDeps)
				m.openEpicField(epicFieldDeps, m.b.Epic("e-wait"))
			},
			want: []string{
				glyphOpen + " e-open (1/5) 開いている箱",
				glyphOpen + " e-stuck (1/4) 詰まった箱",
				glyphOpen + " e-ghost",
				glyphDone + " e-closed (3/3) 閉じた箱 (closed)",
				glyphDone + " e-miss (missing)",
				glyphDone + " e-sat (2/6) 満了した箱 (satisfied)", // the numbers, unlike the other two
			},
			notWant: []string{
				glyphOpen + " e-ghost (missing)",
				glyphDone + " e-ghost", // an open dep never takes the done glyph
				"e-stuck (1/4) STUCK",  // the list marks no dep's stuck state
			},
		},
	} {
		t.Run(tc.surface, func(t *testing.T) {
			m := allDepStatesBoard(t, 400)
			tc.open(m)
			out := frame(m)
			for _, w := range tc.want {
				if !strings.Contains(out, w) {
					t.Errorf("the %s must carry %q", tc.surface, w)
				}
			}
			for _, w := range tc.notWant {
				if strings.Contains(out, w) {
					t.Errorf("the %s must NOT carry %q", tc.surface, w)
				}
			}
		})
	}
}

// furrow's words for a box, in the order both surfaces state them. Nothing
// pinned the ORDER before, on either site, and both slicemode.go and
// boxboardview.go carry comments promising it is the same one — so a
// reordering was a silent, green regression.
//
// bite-exempt: it pins behaviour that already existed; boxHead and boxMeta
// only moved where the composition lives.
func TestTheTwoBoxFactSurfacesStateTheSameChipsInTheSameOrder(t *testing.T) {
	t.Run("the box overview's strip carries the box's whole record", func(t *testing.T) {
		m := allDepStatesBoard(t, 400)
		m.boxesAll = true
		m.openBoxes()
		for _, r := range m.buildBoxes().Rows {
			if r.ID == "e-wait" {
				m.boxesSel = r.Key
			}
		}
		const want = "2/9 done · active · standing · pinned · STUCK · " +
			"repos tomo/a,tomo/b · labels refactor,bug · meta k1,k2"
		if !strings.Contains(frame(m), want) {
			t.Errorf("the strip must carry %q", want)
		}
	})

	// One line, so it drops labels and meta and carries furrow's open_deps
	// count instead — the strip resolves those ids on a line of its own.
	t.Run("the slice panel's readout is the shorter set plus waits-on", func(t *testing.T) {
		m := allDepStatesBoard(t, 400)
		m.toggleSlice()
		m.sliceField = sliceEpic
		m.sliceEpicAll = true
		for i, r := range m.sliceRows() {
			if r.value == "e-wait" {
				m.sliceIdx = i
			}
		}
		const want = "2/9 done · active · standing · pinned · STUCK · " +
			"waits on 3 · repos tomo/a,tomo/b"
		out := frame(m)
		if !strings.Contains(out, want) {
			t.Errorf("the readout must carry %q", want)
		}
		if strings.Contains(out, "labels refactor") || strings.Contains(out, "meta k1") {
			t.Error("the readout is one line: labels and meta belong to the strip alone")
		}
	})
}

// The two dep lines are gated on DIFFERENT fields and it matters. The peek
// renders its line only while furrow still reports an open dep, the same
// open_deps the slice panel counts for →N; the box overview renders over Deps
// and states which edges are settled, because it is the surface where the
// board's handful of epic-to-epic edges are visible at all.
//
// A box whose every dep is satisfied separates them, and nothing pinned it:
// changing the strip's gate to open_deps left the whole repo green (found in
// review of #120).
//
// bite-exempt: it pins behaviour that already existed; the gates did not move.
func TestTheDepLineGatesDifferOnASettledBox(t *testing.T) {
	settled := func(t *testing.T) *board.Board {
		t.Helper()
		return board.NewBoard(
			[]*board.Task{{ID: "t-solo", Title: "箱の中の一枚", Status: "backlog", Priority: 10, Epic: "e-done"}},
			// Deps, but none of them open: furrow's epic_dep_done.
			board.EpicInfo{ID: "e-done", Title: "待ち終わった箱", Done: 1, Total: 3,
				Deps: []string{"e-sat", "e-closed"}},
			board.EpicInfo{ID: "e-sat", Title: "満了した箱", Done: 2, Total: 6},
			board.EpicInfo{ID: "e-closed", Title: "閉じた箱", Done: 3, Total: 3,
				Closed: time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)},
		)
	}

	t.Run("the peek says nothing — the box waits on nothing", func(t *testing.T) {
		m := New(memstore.NewWith(settled(t)), Options{})
		m.Update(tea.WindowSizeMsg{Width: 400, Height: 50})
		m.selectID("t-solo", false)
		press(m, "space")
		if strings.Contains(frame(m), "epic waits on") {
			t.Error("every dep is settled, so the peek shows no line — as the panel shows no arrow")
		}
	})

	t.Run("the box overview still lists them, settled", func(t *testing.T) {
		m := New(memstore.NewWith(settled(t)), Options{})
		m.Update(tea.WindowSizeMsg{Width: 400, Height: 50})
		m.boxesAll = true
		m.openBoxes()
		for _, r := range m.buildBoxes().Rows {
			if r.ID == "e-done" {
				m.boxesSel = r.Key
			}
		}
		out := frame(m)
		if !strings.Contains(out, "waits on") {
			t.Fatal("the strip lists every edge, open or not — it is where the board's epic edges are visible")
		}
		for _, w := range []string{"e-sat (satisfied)", "e-closed (3/3) 閉じた箱 (closed)"} {
			if !strings.Contains(out, w) {
				t.Errorf("the strip must carry %q", w)
			}
		}
	})
}

// The peek's dep STUCK must be a MARKER, not body text — peek.go's own words.
// frame() strips ANSI, so every other assertion in this file is blind to that:
// replacing the warn style with a bare string passes the whole repo (found in
// review of #120). This one reads the styled frame.
//
// The box itself is NOT stuck here, deliberately: the peek's own-epic line
// spells a STUCK of its own in the same style, so a board where both are stuck
// cannot tell which one the assertion found.
//
// bite-exempt: it pins behaviour that already existed.
func TestTheStuckDepMarkerIsStyled(t *testing.T) {
	b := board.NewBoard(
		[]*board.Task{{ID: "t-solo", Title: "箱の中の一枚", Status: "backlog", Priority: 10, Epic: "e-wait"}},
		board.EpicInfo{ID: "e-wait", Title: "待つ箱", Done: 2, Total: 9,
			Deps: []string{"e-stuck"}, OpenDeps: []string{"e-stuck"}},
		board.EpicInfo{ID: "e-stuck", Title: "詰まった箱", Done: 1, Total: 4, Stuck: true},
	)
	m := New(memstore.NewWith(b), Options{})
	m.Update(tea.WindowSizeMsg{Width: 400, Height: 50})
	m.selectID("t-solo", false)
	press(m, "space")

	styled := m.View().Content
	if !strings.Contains(frame(m), "e-stuck (1/4) STUCK 詰まった箱") {
		t.Fatal("setup: the dep line must carry the stuck dep")
	}
	if !strings.Contains(styled, m.th.warn.Render("STUCK")) {
		t.Error("the dep line's STUCK must carry the warn style — dim body text is the one thing it must not be")
	}
}

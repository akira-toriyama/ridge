package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// cursorRow is the overlay list's SELECTED row, trimmed of its padding.
//
// Asserting over the whole frame does not work here and the first version of
// this test got it wrong: enterEdit opens the side-peek, and the peek renders
// every checklist item with a mark of its own, so a Contains over the frame
// matched the peek while the overlay row said the opposite (found in review of
// #121). Pinning the whole row also catches a mark that appends to it.
func cursorRow(t *testing.T, m *Model) string {
	t.Helper()
	for _, l := range strings.Split(frame(m), "\n") {
		i := strings.Index(l, "▌ ")
		if i < 0 {
			continue
		}
		row := l[i:]
		// The overlay is composited over the board, so the line runs on past
		// its right edge. Cut at the edge, not at the first run of spaces: a
		// mark that appended to the row must still be visible here.
		if j := strings.Index(row, "│"); j >= 0 {
			row = row[:j]
		}
		return strings.TrimRight(row, " ")
	}
	t.Fatal("no cursor row in the frame")
	return ""
}

// The four toggle lists — the task's labels and repos, the box's labels and
// repos — over a vocabulary where some rows are carried and some are not.
//
// Written because the mark had NO coverage: `grep '\[x\] '` over every test
// file in the repo returned nothing at all, and no -demo name opens a labels or
// repos list, so the frame sweeps cannot see one either. Inverting all four
// closures on main leaves `go test ./...` green in every package.
//
// The values are Japanese because the real board's are: CLAUDE.md's CJK rule
// is about these very rows, and an ASCII-only fixture is the shape that lets a
// width bug through.
//
// bite-exempt: it pins behaviour that already existed. The four closures
// checkboxMark replaces rendered exactly this; measured, all four lists are
// byte-identical across the change.
func TestTheToggleListsMarkWhatIsCarriedAndWhatIsNot(t *testing.T) {
	b := board.NewBoard(
		[]*board.Task{
			{ID: "t-a", Title: "札のある一枚", Status: "backlog", Priority: 10, Epic: "e-one",
				Labels: []string{"バグ"}, Repos: []string{"tomo/あ"}},
			{ID: "t-b", Title: "別の札", Status: "backlog", Priority: 20,
				Labels: []string{"文書"}, Repos: []string{"tomo/い"}},
		},
		board.EpicInfo{ID: "e-one", Title: "箱", Total: 2,
			Labels: []string{"バグ"}, Repos: []string{"tomo/い"}},
	)
	model := func(t *testing.T) *Model {
		t.Helper()
		m := New(memstore.NewWith(b), Options{})
		m.Update(tea.WindowSizeMsg{Width: 240, Height: 50})
		return m
	}

	// The task's own overlay. t-a carries バグ and tomo/あ; the vocabulary also
	// holds the other task's 文書 and tomo/い, so both marks are reachable in
	// one list and an inverted mark cannot pass by accident.
	for _, tc := range []struct {
		name  string
		field editField
		rows  []string // expected cursor row, per list index
	}{
		{"task labels", fieldLabels, []string{"▌ [x] バグ", "▌ [ ] 文書"}},
		{"task repos", fieldRepos, []string{"▌ [x] tomo/あ", "▌ [ ] tomo/い"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i, want := range tc.rows {
				m := model(t)
				m.selectID("t-a", false)
				m.enterEdit()
				m.openField(tc.field, m.b.Task("t-a"))
				m.edit.listIdx = i
				if got := cursorRow(t, m); got != want {
					t.Errorf("%s row %d = %q, want %q", tc.name, i, got, want)
				}
			}
		})
	}

	// The box's overlay, which spelled the same mark a second way because
	// renderEpicList's own parameter is named `box`.
	for _, tc := range []struct {
		name  string
		field epicField
		rows  []string
	}{
		{"box labels", epicFieldLabels, []string{"▌ [x] バグ", "▌ [ ] 文書"}},
		{"box repos", epicFieldRepos, []string{"▌ [ ] tomo/あ", "▌ [x] tomo/い"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for i, want := range tc.rows {
				m := model(t)
				m.toggleSlice()
				m.sliceField = sliceEpic
				for j, r := range m.sliceRows() {
					if r.value == "e-one" {
						m.sliceIdx = j
					}
				}
				press(m, "m")
				if m.epic == nil {
					t.Fatal("the epic overlay did not open")
				}
				m.openEpicField(tc.field, m.b.Epic("e-one"))
				m.epic.listIdx = i
				if got := cursorRow(t, m); got != want {
					t.Errorf("%s row %d = %q, want %q", tc.name, i, got, want)
				}
			}
		})
	}

	// The checklist's mark is NOT checkboxMark: it indexes the row's own Done
	// rather than testing membership of a set, so two items with the same text
	// can differ. Pinned on the overlay ROW, because the peek renders a mark of
	// its own for the same items into the same frame.
	t.Run("the checklist keeps its own mark", func(t *testing.T) {
		cb := board.NewBoard([]*board.Task{{ID: "t-x", Title: "手順のある一枚", Status: "backlog", Priority: 10,
			Checklist: []board.ChecklistItem{
				{Text: "同じ文言", Done: true},
				{Text: "同じ文言"}, // same text, different state: membership cannot tell them apart
			}}})
		for i, want := range []string{"▌ [x] 同じ文言", "▌ [ ] 同じ文言"} {
			m := New(memstore.NewWith(cb), Options{})
			m.Update(tea.WindowSizeMsg{Width: 240, Height: 50})
			m.selectID("t-x", false)
			m.enterEdit()
			m.openField(fieldChecklist, m.b.Task("t-x"))
			m.edit.listIdx = i
			if got := cursorRow(t, m); got != want {
				t.Errorf("checklist row %d = %q, want %q", i, got, want)
			}
		}
	})
}

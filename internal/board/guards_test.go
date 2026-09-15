package board

import (
	"strings"
	"testing"
)

func guardBoard() *Board {
	return NewBoard([]*Task{
		{ID: "t-real", Title: "実在する一枚", Status: "backlog", Priority: 10,
			Checklist: []ChecklistItem{{Text: "一つ目"}, {Text: "二つ目", Done: true}}},
		{ID: "t-dep", Title: "依存先", Status: "backlog", Priority: 20},
	})
}

// Every write refuses an unknown id in ONE wording. Eleven methods opened with
// their own copy of the guard before mustTask took it, and the WORDING was
// unpinned: six of the eleven arms are reached by existing tests, but every one
// of them asserts only that an error came back. Measured on main — rewording
// all eleven to `no such task: %s` leaves `go test ./...` green in every
// package. The remaining five arms, and all six checklist-bounds arms, are at
// zero coverage outright; the ui cannot reach the bounds ones at all, because
// it pre-checks the index before calling.
//
// bite-exempt: it pins behaviour that already existed. Every guarded write was
// driven past every refusal on both trees before and after, and the error
// strings are byte-identical.
func TestEveryGuardedWriteRefusesAnUnknownIDTheSameWay(t *testing.T) {
	const want = `unknown task "t-ghost"`
	for _, tc := range []struct {
		name string
		call func(*Board) error
	}{
		{"MoveTo", func(b *Board) error { _, err := b.MoveTo("t-ghost", "backlog", 0); return err }},
		{"SetBody", func(b *Board) error { return b.SetBody("t-ghost", "本文") }},
		{"SetFields", func(b *Board) error { return b.SetFields("t-ghost", FieldPatch{}) }},
		{"AppendNote", func(b *Board) error { return b.AppendNote("t-ghost", "追記") }},
		{"Review", func(b *Board) error { return b.Review("t-ghost") }},
		{"DepAdd", func(b *Board) error { return b.DepAdd("t-ghost", "t-dep") }},
		{"DepRm", func(b *Board) error { return b.DepRm("t-ghost", "t-dep") }},
		{"CheckAdd", func(b *Board) error { return b.CheckAdd("t-ghost", "手順") }},
		{"ToggleCheck", func(b *Board) error { return b.ToggleCheck("t-ghost", 0) }},
		{"CheckRm", func(b *Board) error { return b.CheckRm("t-ghost", 0) }},
		{"CheckReword", func(b *Board) error { return b.CheckReword("t-ghost", 0, "手順") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call(guardBoard())
			if err == nil {
				t.Fatalf("%s accepted an unknown id", tc.name)
			}
			if err.Error() != want {
				t.Errorf("%s refused with %q, want %q", tc.name, err, want)
			}
		})
	}
}

// The three checklist writes guard the id BEFORE the index — the bounds test
// dereferences the task the id arm just failed to find, so the order is not a
// preference. An unknown id with a bad index must still say "unknown task".
//
// bite-exempt: pins pre-existing behaviour, as above.
func TestTheChecklistWritesGuardTheIDBeforeTheIndex(t *testing.T) {
	for _, tc := range []struct {
		name string
		call func(*Board, string, int) error
	}{
		{"ToggleCheck", func(b *Board, id string, i int) error { return b.ToggleCheck(id, i) }},
		{"CheckRm", func(b *Board, id string, i int) error { return b.CheckRm(id, i) }},
		{"CheckReword", func(b *Board, id string, i int) error { return b.CheckReword(id, i, "手順") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// Both wrong at once: the id decides.
			if err := tc.call(guardBoard(), "t-ghost", 99); err == nil ||
				err.Error() != `unknown task "t-ghost"` {
				t.Errorf("an unknown id with a bad index refused with %v, want the unknown-task wording", err)
			}
			for _, i := range []int{-1, 2, 99} {
				err := tc.call(guardBoard(), "t-real", i)
				if err == nil {
					t.Fatalf("%s accepted index %d over a two-item checklist", tc.name, i)
				}
				if !strings.Contains(err.Error(), "has no checklist item") {
					t.Errorf("index %d refused with %q", i, err)
				}
			}
		})
	}

	// CheckReword has a third guard, and the bounds one still comes first: a
	// call that is wrong in both ways names the index, not the empty text.
	err := guardBoard().CheckReword("t-real", 99, "  ")
	if err == nil || !strings.Contains(err.Error(), "has no checklist item") {
		t.Errorf("a bad index AND empty text refused with %v, want the index", err)
	}
	if err := guardBoard().CheckReword("t-real", 0, "  "); err == nil ||
		!strings.Contains(err.Error(), "cannot be empty") {
		t.Errorf("empty text at a good index refused with %v", err)
	}
}

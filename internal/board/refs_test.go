package board

import (
	"strings"
	"testing"
)

// Refs are a SEQUENCE (furrow ref --help): adds append at the end in the
// order given, an add already present is a no-op, and a remove is exact-match.
func TestSetFieldsRefsAppendInOrderAndDedupe(t *testing.T) {
	b := NewBoard([]*Task{mk("a", "backlog")})

	if err := b.SetFields("a", FieldPatch{AddRefs: []string{"x.go:1", "https://example.com/spec"}}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(b.Task("a").Refs, ","); got != "x.go:1,https://example.com/spec" {
		t.Errorf("refs = %q, want append order kept", got)
	}

	// Idempotent add: re-adding the first ref must not duplicate or reorder.
	if err := b.SetFields("a", FieldPatch{AddRefs: []string{"x.go:1"}}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(b.Task("a").Refs, ","); got != "x.go:1,https://example.com/spec" {
		t.Errorf("refs after duplicate add = %q, want unchanged", got)
	}

	// An empty add is skipped, matching the other slice fields — furrow
	// itself refuses `--add ""` (exit 2), so the value must never compose.
	if err := b.SetFields("a", FieldPatch{AddRefs: []string{""}}); err != nil {
		t.Fatal(err)
	}
	if got := len(b.Task("a").Refs); got != 2 {
		t.Errorf("an empty add appended: %d refs", got)
	}
}

func TestSetFieldsRefsRemoveIsExactMatchAndNoOpOnAbsent(t *testing.T) {
	b := NewBoard([]*Task{
		{ID: "a", Title: "a", Status: "backlog", Refs: []string{"x.go:1", "x.go:2"}},
	})

	if err := b.SetFields("a", FieldPatch{RmRefs: []string{"x.go:1"}}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(b.Task("a").Refs, ","); got != "x.go:2" {
		t.Errorf("refs = %q, want x.go:2", got)
	}

	// Removing an absent ref is furrow's documented no-op, not an error.
	if err := b.SetFields("a", FieldPatch{RmRefs: []string{"gone.go:9"}}); err != nil {
		t.Errorf("removing an absent ref must be a no-op, got %v", err)
	}
	if got := strings.Join(b.Task("a").Refs, ","); got != "x.go:2" {
		t.Errorf("refs = %q after the no-op, want x.go:2", got)
	}
}

// AppendNote mirrors `furrow note` (v4.0.0 appendBody, re-measured on dev
// 60074b8 against a real store): an empty body becomes the text alone, any
// other body is padded up to AT LEAST one blank line — existing trailing
// newlines are kept, never collapsed — and the result ends in one newline.
func TestAppendNoteMirrorsFurrowsJoin(t *testing.T) {
	cases := []struct {
		name, body, want string
	}{
		{"empty body", "", "追記\n"},
		{"trailing newline", "# 題\n\n本文\n", "# 題\n\n本文\n\n追記\n"},
		{"no trailing newline", "本文", "本文\n\n追記\n"},
		// Measured: furrow pads up to a blank line but never trims one that
		// is already there.
		{"many trailing newlines", "本文\n\n\n", "本文\n\n\n追記\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := NewBoard([]*Task{{ID: "a", Title: "a", Status: "backlog", Body: tc.body}})
			if err := b.AppendNote("a", "追記"); err != nil {
				t.Fatal(err)
			}
			if got := b.Task("a").Body; got != tc.want {
				t.Errorf("body = %q, want %q", got, tc.want)
			}
			if b.Task("a").Updated.IsZero() {
				t.Error("AppendNote must stamp Updated — that is the point of `furrow note`")
			}
		})
	}
}

func TestAppendNoteRefusesWhatFurrowWould(t *testing.T) {
	b := NewBoard([]*Task{{ID: "a", Title: "a", Status: "backlog", Body: "本文\n"}})

	// furrow's "note text is empty" (exit 2), whitespace included.
	for _, text := range []string{"", "   ", "\t"} {
		if err := b.AppendNote("a", text); err == nil {
			t.Errorf("AppendNote(%q) must refuse like furrow's empty-text exit 2", text)
		}
	}
	if got := b.Task("a").Body; got != "本文\n" {
		t.Errorf("a refused note must leave the body untouched: %q", got)
	}

	if err := b.AppendNote("t-nope", "x"); err == nil {
		t.Error("AppendNote on an unknown id must refuse")
	}
}

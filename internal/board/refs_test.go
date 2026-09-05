package board

import (
	"strings"
	"testing"
	"time"
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

	// An empty add is REFUSED, mirroring furrow's own `--add ""` exit 2 —
	// silently skipping it would let the UI think an add landed.
	if err := b.SetFields("a", FieldPatch{AddRefs: []string{""}}); err == nil {
		t.Error("an empty ref add must refuse like furrow's exit 2")
	}
	if got := len(b.Task("a").Refs); got != 2 {
		t.Errorf("a refused add mutated the refs: %d", got)
	}
}

// furrow's --add/--rm are pflag StringArrays since #317 (measured on dev
// 82b181b: `--add 'https://x/?a=1,2' --add 'say "hi"'` lands both, verbatim),
// so a comma or a bare `"` is ordinary ref text here too. This pins that the
// mirror refusal of the CSV era stays gone — its return would refuse refs
// furrow accepts.
func TestSetFieldsRefsKeepCommaAndQuoteVerbatim(t *testing.T) {
	b := NewBoard([]*Task{mk("a", "backlog")})
	refs := []string{
		"https://example.com/spec?rows=1,2",
		`say "hi"`,
	}
	if err := b.SetFields("a", FieldPatch{AddRefs: refs}); err != nil {
		t.Fatalf("SetFields refused a ref furrow takes verbatim: %v", err)
	}
	if got := strings.Join(b.Task("a").Refs, "|"); got != strings.Join(refs, "|") {
		t.Errorf("refs = %q, want both kept verbatim", got)
	}
	if err := b.SetFields("a", FieldPatch{RmRefs: refs[:1]}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(b.Task("a").Refs, "|"); got != `say "hi"` {
		t.Errorf("refs after rm = %q, want the comma'd one gone, exact-match", got)
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

	// furrow's "note text is empty" (exit 2), whitespace included — and `-`,
	// furrow's read-from-stdin marker, which `--` does not neutralize: with
	// ridge's stdin on /dev/null it would refuse AFTER the optimistic apply.
	for _, text := range []string{"", "   ", "\t", "-"} {
		if err := b.AppendNote("a", text); err == nil {
			t.Errorf("AppendNote(%q) must refuse like furrow would", text)
		}
	}
	if got := b.Task("a").Body; got != "本文\n" {
		t.Errorf("a refused note must leave the body untouched: %q", got)
	}

	if err := b.AppendNote("t-nope", "x"); err == nil {
		t.Error("AppendNote on an unknown id must refuse")
	}
}

// SetBody mirrors `furrow edit --body`'s refusal (measured on v5.0.0): an
// empty replacement — whitespace-only included, furrow trims before judging —
// is exit 2, never a silent clear. Refusing before the optimistic apply keeps
// a wiped $EDITOR buffer from landing on screen and then getting yanked by
// the rollback.
func TestSetBodyRefusesWhatFurrowWould(t *testing.T) {
	b := NewBoard([]*Task{{ID: "a", Title: "a", Status: "backlog", Body: "本文\n"}})

	for _, body := range []string{"", "   ", " \n\t"} {
		if err := b.SetBody("a", body); err == nil {
			t.Errorf("SetBody(%q) must refuse like furrow would", body)
		}
	}
	if got := b.Task("a").Body; got != "本文\n" {
		t.Errorf("a refused replacement must leave the body untouched: %q", got)
	}
	if !b.Task("a").Updated.IsZero() {
		t.Error("a refused replacement must not stamp Updated")
	}

	if err := b.SetBody("a", "書き換え\n"); err != nil {
		t.Fatal(err)
	}
	if got := b.Task("a").Body; got != "書き換え\n" {
		t.Errorf("body = %q, want the replacement", got)
	}
	if b.Task("a").Updated.IsZero() {
		t.Error("SetBody must stamp Updated — that is the point of `edit --body`")
	}

	if err := b.SetBody("t-nope", "x"); err == nil {
		t.Error("SetBody on an unknown id must refuse")
	}
}

// Review stamps the review clock ALONE. furrow's ReviewTask writes `reviewed`
// and leaves `updated` where it was ("a review changes no content"), so the
// optimistic half must not bump Updated either — the contract test on the
// real binary pins the store side of the same fact.
func TestReviewStampsReviewedAndLeavesUpdated(t *testing.T) {
	was := time.Date(2026, 7, 1, 0, 0, 0, 0, time.UTC)
	b := NewBoard([]*Task{{ID: "a", Title: "a", Status: "backlog", Updated: was}})
	if err := b.Review("a"); err != nil {
		t.Fatal(err)
	}
	got := b.Task("a")
	if got.Reviewed.IsZero() {
		t.Error("Review must stamp Reviewed")
	}
	if !got.Updated.Equal(was) {
		t.Errorf("Review moved Updated to %v; a review changes no content", got.Updated)
	}
	if err := b.Review("nope"); err == nil {
		t.Error("an unknown id must be refused, not stamped into nothing")
	}
}

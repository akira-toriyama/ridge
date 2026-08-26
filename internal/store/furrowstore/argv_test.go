package furrowstore

import (
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
)

// A title is user free text and furrow's parser reads a leading `-` as a
// shorthand flag, so every command that takes one as a positional needs `--`.
// Both of these ran a real `furrow` against a throwaway store; they skip where
// the binary is absent, like the rest of the contract suite.

// bite-exempt: execs a real furrow binary and always skips where furrow is not
func TestAddAcceptsATitleStartingWithADash(t *testing.T) {
	p, _ := newLabProvider(t)

	const title = "-t は flag ではなくタイトル"
	id, err := p.Add(title, board.AddOptions{Repo: "lab/lab"})
	if err != nil {
		t.Fatalf("add %q: %v", title, err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	got := p.Board().Task(id)
	if got == nil {
		t.Fatalf("%s is not on the board after the add", id)
	}
	if got.Title != title {
		t.Errorf("title landed as %q, want %q", got.Title, title)
	}
}

// The inline-token half of an add (t-69v9): every detail flag mapped in one
// call, verified against furrow's own re-read — the flag spellings are the
// contract under test, so this runs the real binary.
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
func TestAddMapsTheDetailFlags(t *testing.T) {
	p, dir := newLabProvider(t)
	dep := labAdd(t, dir, "依存先")

	id, err := p.Add("詳細つき起票", board.AddOptions{
		Repo:   "lab/lab",
		Value:  4,
		Effort: 2,
		Due:    "+1d",
		Deps:   []string{dep},
		Checks: []string{"再現手順を書く", "直す"},
		Refs:   []string{"internal/ui/addmode.go:1", "https://example.com/x"},
	})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	got := p.Board().Task(id)
	if got == nil {
		t.Fatalf("%s is not on the board after the add", id)
	}
	if got.Value != 4 || got.Effort != 2 {
		t.Errorf("value/effort = %d/%d, want 4/2", got.Value, got.Effort)
	}
	if got.Due.IsZero() {
		t.Error("--due +1d did not land")
	}
	if len(got.Deps) != 1 || got.Deps[0] != dep {
		t.Errorf("deps = %v, want [%s]", got.Deps, dep)
	}
	if len(got.Checklist) != 2 || got.Checklist[0].Text != "再現手順を書く" ||
		got.Checklist[0].Done || got.Checklist[1].Text != "直す" {
		t.Errorf("checklist = %+v, want the two unchecked items verbatim", got.Checklist)
	}
	want := []string{"internal/ui/addmode.go:1", "https://example.com/x"}
	if len(got.Refs) != 2 || got.Refs[0] != want[0] || got.Refs[1] != want[1] {
		t.Errorf("refs = %v, want %v (order kept — refs are a sequence)", got.Refs, want)
	}
}

// The retitle half is the damaging one: the board applies the rename
// optimistically, so a refusal here is watched to land and then get yanked.
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
func TestRetitleAcceptsATitleStartingWithADash(t *testing.T) {
	p, dir := newLabProvider(t)

	id := labAdd(t, dir, "ordinary title")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}

	const title = "--body looking thing"
	want := title
	if err := p.PersistFields(id, board.FieldPatch{Title: &want}); err != nil {
		t.Fatalf("retitle to %q: %v", title, err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := p.Board().Task(id); got == nil || got.Title != title {
		have := "<nil>"
		if got != nil {
			have = got.Title
		}
		t.Errorf("title is %q, want %q", have, title)
	}
}

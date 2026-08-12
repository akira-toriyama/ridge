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

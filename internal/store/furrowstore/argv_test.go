package furrowstore

import (
	"strings"
	"testing"
	"time"

	"github.com/akira-toriyama/ridge/internal/board"
)

// A title is user free text and furrow's parser reads a leading `-` as a
// shorthand flag, so every command that takes one as a positional needs `--`.
// Both of these ran a real `furrow` against a throwaway store; they skip where
// the binary is absent, like the rest of the contract suite.

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

// A body edit must advance the shard's `updated`. Writing bodies/<id>.md
// directly left it stale, so the tracker's staleness signals reported the
// opposite of the truth on exactly the tasks just worked on — and the on-screen
// stamp visibly reverted after the post-write re-read.
func TestPersistBodyAdvancesUpdated(t *testing.T) {
	p, dir := newLabProvider(t)

	id := labAdd(t, dir, "body stamp")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	before := p.Board().Task(id).Updated

	// furrow stamps `updated` at second granularity, so the add and the edit
	// would otherwise land on the same value and prove nothing either way.
	// Bounded by one second, and never flaky: it waits for a boundary rather
	// than guessing a duration.
	waitPastSecond(before)

	if err := p.PersistBody(id, "# 置換\n\n新しい本文\n"); err != nil {
		t.Fatalf("PersistBody: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	got := p.Board().Task(id)
	if !strings.Contains(got.Body, "新しい本文") {
		t.Errorf("body did not persist: %q", got.Body)
	}
	if !got.Updated.After(before) {
		t.Errorf("updated did not advance: %s -> %s — the staleness signals now report the opposite of what happened",
			before.Format("2006-01-02T15:04:05.000Z07:00"), got.Updated.Format("2006-01-02T15:04:05.000Z07:00"))
	}
}

// furrow refuses an empty replacement rather than silently clearing, so
// emptying the buffer in $EDITOR is a failed persist that rolls back.
func TestPersistBodyRefusesAnEmptyReplacement(t *testing.T) {
	p, dir := newLabProvider(t)

	id := labAdd(t, dir, "empty body")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if err := p.PersistBody(id, ""); err == nil {
		t.Error("an empty body replacement was accepted; furrow makes it exit 2, and a silent clear is not a thing to do by accident")
	}
}

// waitPastSecond blocks until the wall clock is strictly past t's second, so a
// second-granularity timestamp taken afterwards is guaranteed to differ.
func waitPastSecond(t time.Time) {
	next := t.Truncate(time.Second).Add(time.Second)
	if d := time.Until(next); d > 0 {
		time.Sleep(d + 10*time.Millisecond)
	}
}

package furrowstore

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
)

// A title is user free text and furrow's parser reads a leading `-` as a
// shorthand flag, so every command that takes one as a positional needs `--`.
// These run a real `furrow` against a throwaway store; they skip where the
// binary is absent, like the rest of the contract suite. TestContract* is not
// decoration: build.yml's contract job runs `-run TestContract` against the
// pinned release, and a lab test outside that name never executes in CI at
// all — the ci job has no furrow and this package is bite-exempt (found by
// review: the draft test's first name left `--draft` with zero CI coverage,
// and three pre-existing tests here had the same hole).

// bite-exempt: execs a real furrow binary and always skips where furrow is not
func TestContractAddAcceptsATitleStartingWithADash(t *testing.T) {
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
func TestContractAddMapsTheDetailFlags(t *testing.T) {
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

// The draft add and its promotion (t-v4pp), against furrow's own re-read.
//
// The lab store gets a `default_repo` FIRST, because without one every add is
// repo-less and the draft assertions hold vacuously — the first cut of this
// test passed with the `--draft` argv line deleted (found by review: furrow's
// auto-attach is the board config's `default_repo`, not the git remote).
// With it set, the control arm proves the flag bites: a plain add lands
// repo-attached, the --draft one lands repo-less, the default `furrow ls`
// hides only the draft, the empty-`-r` load serves both, `is:draft` passes
// through -q, and the promote gesture is nothing draft-specific — the
// existing repo attach.
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
func TestContractAddDraftAndPromoteByRepoAttach(t *testing.T) {
	p, dir := newLabProvider(t)

	// `furrow config set` is the file's one documented writer, and the pinned
	// release carries it (new in v5.0.0). The hand-rolled prepend this
	// replaces existed only because v4.0.0 had no such verb.
	lab(t, dir, "furrow", "config", "set", "default_repo", "lab/lab")

	plain, err := p.Add("素の起票", board.AddOptions{})
	if err != nil {
		t.Fatalf("plain add: %v", err)
	}
	id, err := p.Add("draft の思いつき", board.AddOptions{Draft: true})
	if err != nil {
		t.Fatalf("add --draft: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	// The control arm: auto-attach is live, so an empty repos below is
	// --draft's doing, not the store's resting state.
	if got := p.Board().Task(plain); got == nil || len(got.Repos) != 1 || got.Repos[0] != "lab/lab" {
		t.Fatalf("plain add = %+v, want the default_repo auto-attached — without it every draft assertion is vacuous", got)
	}
	got := p.Board().Task(id)
	if got == nil {
		t.Fatalf("%s is not on the board after the draft add — load must read drafts (empty -r)", id)
	}
	if len(got.Repos) != 0 {
		t.Errorf("repos = %v, want none — --draft suppresses the default_repo", got.Repos)
	}

	// The default read hides exactly the draft — the behavior ridge's empty
	// -r opts out of, pinned so a furrow that stops hiding is news. Not
	// lab(): its CombinedOutput folds the "N draft(s) hidden" stderr note
	// into the JSON, which is the hiding at work but not decodable.
	ls := exec.Command("furrow", "ls", "--json")
	ls.Dir = dir
	var hideNote strings.Builder
	ls.Stderr = &hideNote
	lsOut, err := ls.Output()
	if err != nil {
		t.Fatalf("furrow ls: %v", err)
	}
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(lsOut, &rows); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == id {
			t.Errorf("default `furrow ls` serves the draft %s — it is documented to hide drafts", id)
		}
	}
	if !strings.Contains(hideNote.String(), "draft") {
		t.Errorf("stderr = %q, want the drafts-hidden note", hideNote.String())
	}

	ids, err := p.Query("is:draft")
	if err != nil {
		t.Fatalf("is:draft: %v", err)
	}
	found := false
	for _, x := range ids {
		found = found || x == id
	}
	if !found {
		t.Errorf("is:draft = %v, want it to serve %s", ids, id)
	}

	// Promotion: the repo attach the edit overlay already issues.
	if err := p.PersistFields(id, board.FieldPatch{AddRepos: []string{"lab/lab"}}); err != nil {
		t.Fatalf("promote via repo attach: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	got = p.Board().Task(id)
	if got == nil || len(got.Repos) != 1 || got.Repos[0] != "lab/lab" {
		t.Errorf("after the promote, task = %+v, want repos [lab/lab]", got)
	}
}

// The retitle half is the damaging one: the board applies the rename
// optimistically, so a refusal here is watched to land and then get yanked.
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
func TestContractRetitleAcceptsATitleStartingWithADash(t *testing.T) {
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

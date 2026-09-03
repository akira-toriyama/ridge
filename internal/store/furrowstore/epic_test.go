package furrowstore

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
)

// Contract tests for the epic write family: a real furrow binary against a
// throwaway store, like the rest of the suite. They pin the argv facts the
// adapter composes, each of which was measured against the release
// .github/workflows/build.yml installs for the contract job — v4.0.0
// originally, re-measured on v5.0.0 when the pin moved. Every one
// of them is a spelling furrow refuses if it is composed the other way, and none
// of them is visible from ridge's own tests.

// labEpic creates a box and returns its id. repos is passed through verbatim so
// a caller can create a REPO-LESS box: `-r ""` does not clear an earlier `-r`
// (measured: `epic add -r lab/lab -r ""` still answers repos ["lab/lab"]), so
// the only way to one is to omit the flag entirely.
func labEpic(t *testing.T, dir, title string, repos ...string) string {
	t.Helper()
	args := []string{"epic", "add", title, "--json"}
	for _, r := range repos {
		args = append(args, "-r", r)
	}
	out := lab(t, dir, "furrow", args...)
	var row addRow
	if err := json.Unmarshal(out, &row); err != nil || row.ID == "" {
		t.Fatalf("epic add %q: undecodable %q (%v)", title, out, err)
	}
	return row.ID
}

// wantKind asserts furrow refused with a specific error kind. The kind is the
// documented branch key (`furrow vocab error-kinds`); asserting only "err !=
// nil" let a test pass on the WRONG refusal — the repo-less activate case was
// green while furrow was really answering epic-active-clash.
func wantKind(t *testing.T, err error, kind string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want a %s refusal, got nil", kind)
	}
	var fe *furrowError
	if !errors.As(err, &fe) {
		t.Fatalf("want a %s refusal, got a non-envelope error: %v", kind, err)
	}
	if fe.Kind != kind {
		t.Errorf("refusal kind = %q, want %q (%v)", fe.Kind, kind, err)
	}
}

// bite-exempt: execs a real furrow binary and always skips where furrow is not
func TestContractEpicAddAndSetRoundTrip(t *testing.T) {
	p, dir := newLabProvider(t)
	labAdd(t, dir, "種タスク") // seeds lab/lab as a known repo

	// `epic add --json` answers ONE object shaped like an `epic ls` row (minus
	// progress/stuck), and a leading dash in the title needs `--` exactly as
	// `add` and `retitle` do.
	const dashTitle = "-r は flag ではなく箱の名前"
	id, err := p.EpicAdd(dashTitle, board.EpicAddOptions{
		Goal: "最初のゴール", Labels: []string{"tui"}, Repos: []string{"lab/lab"},
	})
	if err != nil {
		t.Fatalf("epic add %q: %v", dashTitle, err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	got := p.Board().Epic(id)
	if got == nil {
		t.Fatalf("%s is not on the board after the add", id)
	}
	if got.Title != dashTitle {
		t.Errorf("title landed as %q, want %q", got.Title, dashTitle)
	}
	if got.Goal != "最初のゴール" || !contains(got.Labels, "tui") || !contains(got.Repos, "lab/lab") {
		t.Errorf("add options did not map: %+v", got)
	}
	if got.Active {
		t.Error("a new box must never be active — opening one is a separate act")
	}

	// One `epic set` carries the whole patch. --standing/--pinned are bools, so
	// they must be spelled as ONE token (`--standing=false`): the space form is
	// exit 2 ("accepts 1 arg(s), received 2").
	title, goal := "改名した箱", ""
	yes, no := true, false
	if err := p.EpicSet(id, board.EpicPatch{
		Title: &title, Goal: &goal, Standing: &yes, Pinned: &no,
		AddLabels: []string{"box"}, RmLabels: []string{"tui"},
		SetMeta: map[string]string{"origin": "contract", "zzz": "last"},
	}); err != nil {
		t.Fatalf("epic set: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	got = p.Board().Epic(id)
	switch {
	case got.Title != title:
		t.Errorf("title = %q, want %q", got.Title, title)
	case got.Goal != "":
		t.Errorf(`goal = %q, want "" — --goal "" is furrow's clear`, got.Goal)
	case !got.Standing:
		t.Error("--standing=true did not land")
	case got.Pinned:
		t.Error("--pinned=false did not land")
	case !contains(got.Labels, "box") || contains(got.Labels, "tui"):
		t.Errorf("labels = %v, want [box]", got.Labels)
	case got.Meta["origin"] != "contract" || got.Meta["zzz"] != "last":
		t.Errorf("meta = %v, want origin=contract zzz=last", got.Meta)
	}

	// --rm-meta takes the KEY, not k=v.
	if err := p.EpicSet(id, board.EpicPatch{RmMeta: []string{"zzz"}}); err != nil {
		t.Fatalf("epic set --rm-meta: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if _, still := p.Board().Epic(id).Meta["zzz"]; still {
		t.Error("--rm-meta left the key behind")
	}

	// The repo flags are their OWN pair (--add-repo/--rm-repo), not the task
	// side's --add/--rm: swapping them for anything else rode the whole suite
	// green before this case existed, and repos are what decides whether a box
	// can be activated at all.
	if err := p.EpicSet(id, board.EpicPatch{AddRepos: []string{"lab/other"}}); err != nil {
		t.Fatalf("epic set --add-repo: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := p.Board().Epic(id).Repos; !contains(got, "lab/other") || !contains(got, "lab/lab") {
		t.Errorf("repos = %v, want both after --add-repo", got)
	}
	if err := p.EpicSet(id, board.EpicPatch{RmRepos: []string{"lab/other"}}); err != nil {
		t.Fatalf("epic set --rm-repo: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := p.Board().Epic(id).Repos; contains(got, "lab/other") {
		t.Errorf("repos = %v, want lab/other detached", got)
	}

	// --pinned=true, not just the false half: hard-coding the flag rode green.
	yesPinned := true
	if err := p.EpicSet(id, board.EpicPatch{Pinned: &yesPinned}); err != nil {
		t.Fatalf("epic set --pinned=true: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if !p.Board().Epic(id).Pinned {
		t.Error("--pinned=true did not land")
	}

	// An empty patch never reaches furrow: `epic set` with no change flag is
	// exit 2, so a no-op gesture would report a store error.
	if err := p.EpicSet(id, board.EpicPatch{}); err == nil {
		t.Error("an empty patch must be refused before it is sent")
	}
	// An empty title is furrow's refusal, and unlike --goal it has no clearing
	// meaning.
	empty := ""
	if err := p.EpicSet(id, board.EpicPatch{Title: &empty}); err == nil {
		t.Error(`--title "" must refuse; only --goal "" clears`)
	}
}

// bite-exempt: execs a real furrow binary and always skips where furrow is not
func TestContractEpicActivateHoldsOneSlotPerRepo(t *testing.T) {
	p, dir := newLabProvider(t)
	labAdd(t, dir, "種タスク")
	first := labEpic(t, dir, "先に開く箱", "lab/lab")
	second := labEpic(t, dir, "あとから開こうとする箱", "lab/lab")
	orphan := labEpic(t, dir, "repo を持たない箱")

	const reason = "contract: 誰が頼んだかの記録"
	if err := p.EpicActivate(first, reason); err != nil {
		t.Fatalf("activate %s: %v", first, err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if e := p.Board().Epic(first); e == nil || !e.Active {
		t.Fatalf("%s did not become active", first)
	}

	// --reason is the whole point of collecting one: furrow appends it to the
	// box's own body as the activation record, which is what keeps a switch
	// visible to the next session. Dropping the flag rode the suite green.
	// The path is this test's own t.TempDir() plus an id furrow just minted (G304).
	body, err := os.ReadFile(filepath.Join(dir, ".furrow", "bodies", first+".md")) //nolint:gosec // G304: the test's own tempdir
	if err != nil {
		t.Fatalf("reading %s's body: %v", first, err)
	}
	if !strings.Contains(string(body), reason) {
		t.Errorf("the activation reason is not in the box's body:\n%s", body)
	}

	// The rule ridge must never present as a toggle: a second box for the same
	// repo is REFUSED, not swapped in. The KIND matters — the overlay's
	// precondition line exists to predict exactly this one.
	wantKind(t, p.EpicActivate(second, ""), "epic-active-clash")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if e := p.Board().Epic(first); e == nil || !e.Active {
		t.Error("the refused activate stole the incumbent's slot")
	}
	if e := p.Board().Epic(second); e == nil || e.Active {
		t.Error("the refused activate marked the second box active anyway")
	}

	// A repo-less box cannot be activated at all: it would bypass the count.
	// A DIFFERENT kind, which is why the overlay says "attach a repo first"
	// rather than naming an incumbent.
	if e := p.Board().Epic(orphan); e == nil || len(e.Repos) != 0 {
		t.Fatalf("setup: %s should name no repo, got %+v", orphan, e)
	}
	wantKind(t, p.EpicActivate(orphan, ""), "validation")

	// deactivate frees the slot and answers with the previous-active
	// suggestion, computed fresh from the activation log.
	if _, err := p.EpicDeactivate(first); err != nil {
		t.Fatalf("deactivate %s: %v", first, err)
	}
	if err := p.EpicActivate(second, ""); err != nil {
		t.Fatalf("the slot was not freed: %v", err)
	}
	prev, err := p.EpicDeactivate(second)
	if err != nil {
		t.Fatalf("deactivate %s: %v", second, err)
	}
	if prev.ID != first {
		t.Errorf("previous = %+v, want %s — the suggestion comes from the activation log", prev, first)
	}
}

// bite-exempt: execs a real furrow binary and always skips where furrow is not
func TestContractEpicDepAddAndRm(t *testing.T) {
	p, dir := newLabProvider(t)
	labAdd(t, dir, "種タスク")
	waiter := labEpic(t, dir, "あとで開く箱", "lab/lab")
	blocker := labEpic(t, dir, "先に閉じる箱", "lab/lab")

	if err := p.EpicDepAdd(waiter, blocker); err != nil {
		t.Fatalf("epic dep: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	e := p.Board().Epic(waiter)
	if e == nil || len(e.Deps) != 1 || e.Deps[0] != blocker {
		t.Fatalf("deps = %+v, want [%s]", e, blocker)
	}
	if len(e.OpenDeps) != 1 || e.OpenDeps[0] != blocker {
		t.Errorf("OpenDeps = %v, want [%s] — furrow derives it, ridge never recomputes", e.OpenDeps, blocker)
	}

	// Acyclic is furrow's rule, and it is the reason the overlay can offer a
	// free-text dep add without a local cycle check.
	if err := p.EpicDepAdd(blocker, waiter); err == nil {
		t.Error("the cycle-closing epic dep was accepted; furrow must refuse it")
	}

	// A dep REF is user free text (furrow resolves an id, a unique prefix or a
	// unique title substring), so it must ride behind `--`. Without the guard a
	// dash-leading ref is parsed as a flag: `epic dep <id> --rm` becomes
	// "requires at least 2 arg(s)" instead of a refusal naming the ref.
	if err := p.EpicDepAdd(waiter, "--rm"); err == nil {
		t.Error("a dash-leading dep ref was accepted")
	} else {
		wantKind(t, err, "epic-not-found")
	}

	if err := p.EpicDepRm(waiter, blocker); err != nil {
		t.Fatalf("epic dep --rm: %v", err)
	}
	// Removing an edge that is not there is furrow's refusal, not a no-op —
	// unlike adding, which is idempotent.
	if err := p.EpicDepRm(waiter, blocker); err == nil {
		t.Error("removing an absent epic dep was accepted; furrow refuses it")
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if e := p.Board().Epic(waiter); len(e.Deps) != 0 {
		t.Errorf("deps = %v, want none after the rm", e.Deps)
	}
}

// The lifecycle pair against the real CLI. `done` is in v4.0.0 but `reopen`
// arrived only in v5.0.0, which is the release build.yml pins — on the older
// pin this test fails with kind "validation" for `unknown command "reopen"`,
// indistinguishable by Kind from a legitimate refusal, so the pin and this
// test move together.
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
// on PATH — which is CI, so the gate can never judge it there
func TestContractEpicDoneAndReopenRoundTrip(t *testing.T) {
	p, dir := newLabProvider(t)
	labAdd(t, dir, "既存のタスク") // seeds lab/lab as a known repo
	id := labEpic(t, dir, "閉じたり開いたりする箱", "lab/lab")
	if err := p.EpicActivate(id, "契約テスト"); err != nil {
		t.Fatalf("activate: %v", err)
	}

	if _, err := p.EpicDone(id); err != nil {
		t.Fatalf("epic done: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	box := p.Board().Epic(id)
	if box == nil || box.Closed.IsZero() {
		t.Fatalf("a closed box must reach the snapshot WITH its stamp: %+v", box)
	}
	// Closing an ACTIVE box vacates the slot in the same write. ridge shows
	// the flag, so a stale `active: yes` on a closed box would be a lie the
	// overlay renders.
	if box.Active {
		t.Error("furrow clears the active flag when it closes the box; the snapshot kept it")
	}
	for _, e := range p.Board().Epics() {
		if e.ID == id {
			t.Error("a closed box must stay out of the default population")
		}
	}

	if err := p.EpicReopen(id); err != nil {
		t.Fatalf("epic reopen: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	box = p.Board().Epic(id)
	if box == nil || !box.Closed.IsZero() {
		t.Fatalf("reopen must clear the stamp: %+v", box)
	}
	// The half a "it is open again" assertion would miss: furrow reopens
	// INACTIVE, and ridge must not grow a second write that re-activates.
	if box.Active {
		t.Error("reopen re-activated the box; furrow never chains the two")
	}
	back := false
	for _, e := range p.Board().Epics() {
		back = back || e.ID == id
	}
	if !back {
		t.Error("a reopened box must return to the default population")
	}

	// Both no-op directions are refusals, not silent successes, and the KIND
	// is what a caller may branch on.
	if _, err := p.EpicDone(id); err != nil {
		t.Fatalf("closing an open box must still work: %v", err)
	}
	_, err := p.EpicDone(id)
	wantKind(t, err, "validation")
	if err := p.EpicReopen(id); err != nil {
		t.Fatalf("reopening a closed box must work: %v", err)
	}
	wantKind(t, p.EpicReopen(id), "validation")
}

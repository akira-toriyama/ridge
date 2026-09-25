package furrowstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/ui"
)

// Contract tests: a real furrow binary against a throwaway store in a
// tempdir. They prove the CLI/JSON mapping — everything model-side is proved
// by the fixture tests. Skipped wherever furrow is not on PATH (CI), and in
// -short runs.

func newLabProvider(t *testing.T) (*Store, string) {
	t.Helper()
	if testing.Short() {
		t.Skip("contract tests exec furrow ~20 times; skipped in -short")
	}
	if _, err := exec.LookPath("furrow"); err != nil {
		t.Skip("furrow binary not on PATH")
	}
	// The throwaway store is only throwaway if furrow cannot be pointed
	// somewhere else, and every command below inherits this process's env.
	// FURROW_DIR outranks the working directory outright; FURROW_BOARD outranks
	// it only for `init`, which is enough, because init is what decides where
	// the store the rest of the suite asserts against gets made. Measured on
	// v5.0.0: under an exported FURROW_DIR, `furrow init` seeds THAT directory
	// or refuses at it, so a developer with one exported would have this suite
	// asserting about — and mutating — a real board. Empty reads as unset.
	t.Setenv("FURROW_DIR", "")
	t.Setenv("FURROW_BOARD", "")

	dir := t.TempDir()
	lab(t, dir, "git", "init", "-q")
	lab(t, dir, "furrow", "init")

	p := &Store{c: &furrowClient{bin: "furrow", dir: dir, timeout: 15 * time.Second}}
	return p, dir
}

// lab runs one command inside the throwaway store, failing the test on any
// non-zero exit — a broken seed makes every later assertion a lie.
func lab(t *testing.T, dir string, name string, args ...string) []byte {
	t.Helper()
	cmd := exec.Command(name, args...) //nolint:gosec // seeding the throwaway store IS the test
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return out
}

// labAdd creates one task. A single `add --json` answers with ONE object
// (only --stdin bulk answers with an array) — a shape the contract suite
// itself discovered.
func labAdd(t *testing.T, dir, title string, extra ...string) string {
	t.Helper()
	args := append([]string{"add", title, "-r", "lab/lab", "--json"}, extra...)
	out := lab(t, dir, "furrow", args...)
	var row struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &row); err != nil || row.ID == "" {
		t.Fatalf("add %q: undecodable %q (%v)", title, out, err)
	}
	return row.ID
}

func labLaneOrder(t *testing.T, dir, lane string) []string {
	t.Helper()
	out := lab(t, dir, "furrow", "ls", "-r", "", "-s", lane, "--json")
	var rows []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatal(err)
	}
	ids := make([]string, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	return ids
}

// The done-lane assertion here was changed from a tautology to a real check on
// furrow's done_lane mapping; it DOES bite (dropping `Done: name ==
// cfg.DoneLane` makes it report `""`), but only where a furrow binary exists to
// build a store from. The `contract` job supplies one and runs it for real.
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
func TestContractLoadMapsTheStore(t *testing.T) {
	p, dir := newLabProvider(t)

	t1 := labAdd(t, dir, "最初のタスク", "--value", "4", "--effort", "2", "-l", "tui",
		"--check", "一つ目", "--check", "二つ目", "--due", "2199-01-02")
	t2 := labAdd(t, dir, "二番目のタスク")
	lab(t, dir, "furrow", "check", t1, "0")
	lab(t, dir, "furrow", "dep", t2, t1)
	lab(t, dir, "furrow", "epic", "add", "箱", "-r", "lab/lab", "--goal", "テスト用")
	lab(t, dir, "furrow", "set", t1, "-e", "箱")

	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	b := p.Board()

	if !b.Writable() {
		t.Error("a fresh store must be writable")
	}
	if b.Lane("inbox") == nil || b.DoneLane() == "" {
		t.Errorf("lane vocabulary not mapped: %+v", b.Lanes())
	}
	// DoneLane() finds the lane whose Done flag is set, so asserting that
	// lane's Done flag proves nothing. Assert the MAPPING instead: furrow
	// reports done_lane and ridge must mark that lane. Dropping
	// `Done: name == cfg.DoneLane` makes this return "".
	if got := b.DoneLane(); got != "done" {
		t.Errorf("furrow's done_lane mapped to %q, want \"done\"", got)
	}

	x := b.Task(t1)
	if x == nil {
		t.Fatalf("task %s missing from Board()", t1)
	}
	if x.Title != "最初のタスク" || x.Value != 4 || x.Effort != 2 {
		t.Errorf("scalar fields wrong: %+v", x)
	}
	if len(x.Labels) != 1 || x.Labels[0] != "tui" {
		t.Errorf("labels = %v", x.Labels)
	}
	if len(x.Checklist) != 2 || !x.Checklist[0].Done || x.Checklist[1].Done {
		t.Errorf("checklist = %+v", x.Checklist)
	}
	if x.Due.IsZero() {
		t.Error("due did not map")
	}
	if x.Epic == "" || b.Epic(x.Epic) == nil || b.Epic(x.Epic).Title != "箱" {
		t.Errorf("epic membership did not resolve: epic=%q", x.Epic)
	}
	if y := b.Task(t2); y == nil || len(y.Deps) != 1 || y.Deps[0] != t1 {
		t.Errorf("deps did not map: %+v", y)
	}
	if x.Body == "" {
		t.Error("ls --json must carry the body (the peek renders it)")
	}
}

func TestContractPersistMoveAnchorsAndRespace(t *testing.T) {
	p, dir := newLabProvider(t)
	a := labAdd(t, dir, "a")
	b := labAdd(t, dir, "b")
	c := labAdd(t, dir, "c")

	// The lane's only card: no anchor.
	if _, err := p.PersistMove(a, "ready", "", ""); err != nil {
		t.Fatal(err)
	}
	// Anchored placements, both directions.
	if _, err := p.PersistMove(b, "ready", a, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := p.PersistMove(c, "ready", "", a); err != nil {
		t.Fatal(err)
	}
	if got := labLaneOrder(t, dir, "ready"); strings.Join(got, ",") != b+","+a+","+c {
		t.Errorf("ready order = %v, want [%s %s %s]", got, b, a, c)
	}

	// Exhaust the gap between two neighbours: the store must respace and say so.
	lab(t, dir, "furrow", "set", b, "-p", "100")
	lab(t, dir, "furrow", "set", a, "-p", "101")
	rep, err := p.PersistMove(c, "ready", a, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Renumbered) == 0 {
		t.Error("an exhausted gap must report renumbered neighbours")
	}
	if rep.Repeat != nil {
		t.Errorf("a placement outside the done lane reported a series: %+v", rep.Repeat)
	}
}

func TestContractPersistDoneCheckBody(t *testing.T) {
	p, dir := newLabProvider(t)
	id := labAdd(t, dir, "仕上げ対象", "--check", "項目")

	rep, err := p.PersistDone(id)
	if err != nil {
		t.Fatal(err)
	}
	if rep != nil {
		t.Errorf("a close of a task with no rule reported a series: %+v", rep)
	}
	if err := p.PersistCheck(id, 0, true); err != nil {
		t.Fatal(err)
	}
	if err := p.PersistBody(id, "# 仕上げ対象\n\n置換された本文\n"); err != nil {
		t.Fatal(err)
	}

	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	x := p.Board().Task(id)
	if x == nil {
		t.Fatal("task vanished")
	}
	if x.Status != p.Board().DoneLane() || x.Closed.IsZero() {
		t.Errorf("done did not stamp: status=%s closed=%v", x.Status, x.Closed)
	}
	if !x.Checklist[0].Done {
		t.Error("check did not persist")
	}
	if !strings.Contains(x.Body, "置換された本文") {
		t.Errorf("body did not persist: %q", x.Body)
	}
	// The replaced body lives where furrow says bodies live.
	raw, err := os.ReadFile(filepath.Join(dir, ".furrow", "bodies", id+".md")) //nolint:gosec // the test's own tempdir
	if err != nil || !strings.Contains(string(raw), "置換された本文") {
		t.Errorf("body file wrong: %v %q", err, raw)
	}

	if err := p.PersistCheck(id, 0, false); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if p.Board().Task(id).Checklist[0].Done {
		t.Error("check --off did not persist")
	}
}

// The reason PersistBody execs `edit --body -` instead of writing the body
// file directly: the direct write left the shard's `updated` stale, so other
// machines' newness checks missed body edits (t-t9ac).
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
// on PATH — which is CI, so the gate can never judge it there
func TestContractPersistBodyAdvancesUpdated(t *testing.T) {
	p, dir := newLabProvider(t)
	id := labAdd(t, dir, "本文の対象")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	before := p.Board().Task(id).Updated
	if before.IsZero() {
		t.Fatal("the add must stamp updated")
	}

	// furrow stamps at second resolution and truncates, so the add above and
	// the edit below can share a second; wait out the boundary or "advanced"
	// is a coin flip.
	time.Sleep(time.Until(before.Add(1100 * time.Millisecond)))

	if err := p.PersistBody(id, "# 本文の対象\n\n置換後の本文\n"); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	x := p.Board().Task(id)
	if !x.Updated.After(before) {
		t.Errorf("updated did not advance: %v -> %v", before, x.Updated)
	}
	if !strings.Contains(x.Body, "置換後の本文") {
		t.Errorf("body did not persist: %q", x.Body)
	}

	// The refusal Board.SetBody mirrors: an empty replacement — whitespace
	// included, furrow trims before judging — is exit 2, never a clear. If
	// this ever starts passing, the mirror upstream is refusing a write
	// furrow takes.
	if err := p.PersistBody(id, " \n"); err == nil {
		t.Error("a whitespace-only replacement must be furrow's refusal, not a write")
	}
}

// The full loop, for real: a live Program over a real store, a keyboard
// gesture, and the store itself as the assertion target. This is the one test
// where the optimistic queue's Cmd actually runs inside bubbletea's loop and
// the reconcile re-read lands.
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
// on PATH — which is CI, so the gate can never judge it there
func TestContractProgramMovesForReal(t *testing.T) {
	p, dir := newLabProvider(t)
	id := labAdd(t, dir, "動かす対象")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}

	m := ui.New(p, ui.Options{})
	var in bytes.Buffer
	in.WriteString("L") // cycle the selected card one lane forward
	// Give the persist queue's Cmds time to run inside the program loop
	// before quitting: q arrives after the input above in the same script,
	// but bubbletea processes queued Cmd results before Quit tears down.
	in.WriteString("q")
	var out bytes.Buffer
	if _, err := tea.NewProgram(m,
		tea.WithInput(&in), tea.WithOutput(&out),
		tea.WithoutSignals(), tea.WithWindowSize(140, 40),
	).Run(); err != nil {
		t.Fatalf("program: %v", err)
	}

	got := labLaneOrder(t, dir, "backlog")
	if len(got) != 1 || got[0] != id {
		t.Fatalf("store backlog = %v, want [%s] — the gesture never reached furrow", got, id)
	}
}

func TestContractErrorsCarryTheEnvelope(t *testing.T) {
	p, _ := newLabProvider(t)

	_, err := p.PersistMove("t-nope", "ready", "", "")
	var fe *furrowError
	if !errors.As(err, &fe) || fe.Kind == "" {
		t.Fatalf("want a decoded furrowError with a kind, got %T %v", err, err)
	}
	if fe.Retryable {
		t.Error("an unknown id must not claim to be retryable")
	}

	id := labAdd(t, p.c.dir, "存在する")
	if _, err := p.PersistMove(id, "no-such-lane", "", ""); err == nil {
		t.Error("an unknown lane must error")
	}
}

func contains(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

func TestContractQueryPassesThroughAndRefuses(t *testing.T) {
	p, dir := newLabProvider(t)
	blocker := labAdd(t, dir, "先にやる方")
	blocked := labAdd(t, dir, "待つ方")
	lab(t, dir, "furrow", "dep", blocked, blocker)
	free := labAdd(t, dir, "自由な方", "-l", "cli")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}

	// The grammar is furrow's: is:blocked, labels, CJK free text all resolve
	// store-side and come back as ids ridge can intersect with its snapshot.
	ids, err := p.Query("is:blocked")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != blocked {
		t.Errorf("is:blocked = %v, want [%s]", ids, blocked)
	}
	ids, err = p.Query("label:cli")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != free {
		t.Errorf("label:cli = %v, want [%s]", ids, free)
	}
	ids, err = p.Query("自由")
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != free {
		t.Errorf("CJK free text = %v, want [%s]", ids, free)
	}

	// An empty result is exit 0 + [], never an error.
	ids, err = p.Query("label:no-such-label")
	if err != nil || len(ids) != 0 {
		t.Errorf("empty result = (%v, %v), want ([], nil)", ids, err)
	}

	// A malformed query is furrow's refusal (exit 2), surfaced as an error —
	// the model shows it and keeps the last good verdict.
	if _, err := p.Query("nope:x"); err == nil {
		t.Error("a malformed query must refuse, not match nothing")
	}
}

func TestContractPersistFieldsAndChecklistEdits(t *testing.T) {
	p, dir := newLabProvider(t)
	id := labAdd(t, dir, "編集される方")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}

	iv := func(n int) *int { return &n }
	sv := func(s string) *string { return &s }

	// The set-shaped fields compose into one write; read the store back and
	// believe IT, not the patch.
	if err := p.PersistFields(id, board.FieldPatch{
		Value: iv(4), Effort: iv(2),
		AddLabels: []string{"cli", "tui"},
		Due:       sv("2026-09-01"),
	}); err != nil {
		t.Fatal(err)
	}
	// Title and repo edits are their own commands behind the same patch; a
	// full owner/repo attaches (short names must already be known — furrow
	// never invents a repo silently).
	if err := p.PersistFields(id, board.FieldPatch{
		Title:    sv("編集後のタイトル"),
		AddRepos: []string{"lab/other"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	got := p.Board().Task(id)
	if got.Value != 4 || got.Effort != 2 {
		t.Errorf("estimates = %d/%d, want 4/2", got.Value, got.Effort)
	}
	if len(got.Labels) != 2 {
		t.Errorf("labels = %v", got.Labels)
	}
	// In the board's calendar: the lab declares none, so furrow bound the bare
	// day in the process zone, and west of UTC that instant is the 2nd in UTC
	// (found by review — it failed under TZ=America/New_York on main too).
	if got.Due.In(board.Zone()).Format("2006-01-02") != "2026-09-01" {
		t.Errorf("due = %v", got.Due)
	}
	if got.Title != "編集後のタイトル" {
		t.Errorf("title = %q", got.Title)
	}
	if !contains(got.Repos, "lab/other") {
		t.Errorf("repos = %v, want lab/other attached", got.Repos)
	}

	// Clears map to the --clear-* flags.
	if err := p.PersistFields(id, board.FieldPatch{
		Value: iv(0), Due: sv(""), RmLabels: []string{"cli"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	got = p.Board().Task(id)
	if got.Value != 0 || !got.Due.IsZero() || len(got.Labels) != 1 {
		t.Errorf("after clears: value=%d due=%v labels=%v", got.Value, got.Due, got.Labels)
	}

	// Checklist add / reword / toggle / rm, by index.
	for _, text := range []string{"最初の項目", "二番目の項目"} {
		if err := p.PersistCheckAdd(id, text); err != nil {
			t.Fatal(err)
		}
	}
	if err := p.PersistCheckReword(id, 1, "書き直した項目"); err != nil {
		t.Fatal(err)
	}
	if err := p.PersistCheck(id, 0, true); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	cl := p.Board().Task(id).Checklist
	if len(cl) != 2 || !cl[0].Done || cl[1].Text != "書き直した項目" {
		t.Fatalf("checklist = %+v", cl)
	}
	if err := p.PersistCheckRm(id, 0); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	cl = p.Board().Task(id).Checklist
	if len(cl) != 1 || cl[0].Text != "書き直した項目" {
		t.Fatalf("after rm: %+v", cl)
	}
}

func TestContractAddMapsTheContext(t *testing.T) {
	p, dir := newLabProvider(t)
	labAdd(t, dir, "既存のタスク") // seeds lab/lab as a known repo
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	epicOut := lab(t, dir, "furrow", "epic", "add", "箱", "--json")
	var epic struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(epicOut, &epic); err != nil || epic.ID == "" {
		t.Fatalf("epic add: %v (%s)", err, epicOut)
	}

	id, err := p.Add("文脈つきで起票", board.AddOptions{
		Lane: "backlog", Label: "tui", Epic: epic.ID, Repo: "lab/lab",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	got := p.Board().Task(id)
	if got == nil {
		t.Fatalf("added %s but the store does not serve it", id)
	}
	if got.Status != "backlog" || !contains(got.Labels, "tui") ||
		got.Epic != epic.ID || !contains(got.Repos, "lab/lab") {
		t.Errorf("context did not map: %+v", got)
	}

	// An empty title is furrow's refusal, not a silent draft.
	if _, err := p.Add("", board.AddOptions{}); err == nil {
		t.Error("an empty title must refuse")
	}
}

// Epic deps travel `epic ls --all --json`'s deps array, and open_deps arrives
// alongside as a furrow-DERIVED field (like progress and stuck): the deps
// still waiting, with deps on closed epics already resolved away. ridge
// consumes both verbatim and recomputes neither — and, since the read is
// --all, the closed dep this seeds is a row ridge can point at rather than an
// id it has to shrug about.
//
// bite-exempt: execs a real furrow binary and always skips where furrow is
// not on PATH — which is CI, so the gate can never judge it there
func TestContractEpicDepsReachTheSnapshot(t *testing.T) {
	p, dir := newLabProvider(t)
	labAdd(t, dir, "既存のタスク") // seeds lab/lab as a known repo

	addEpic := func(title string) string {
		out := lab(t, dir, "furrow", "epic", "add", title, "--json")
		var e struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(out, &e); err != nil || e.ID == "" {
			t.Fatalf("epic add: %v (%s)", err, out)
		}
		return e.ID
	}
	dep := addEpic("先に閉じる箱")
	closedDep := addEpic("もう閉じた箱")
	box := addEpic("待つ箱")
	lab(t, dir, "furrow", "epic", "dep", box, dep, closedDep)
	lab(t, dir, "furrow", "epic", "done", closedDep)

	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	b := p.Board()
	e := b.Epic(box)
	if e == nil {
		t.Fatalf("epic %s did not reach the snapshot", box)
	}
	if len(e.Deps) != 2 || !contains(e.Deps, dep) || !contains(e.Deps, closedDep) {
		t.Errorf("Deps = %v, want both %s and %s", e.Deps, dep, closedDep)
	}
	// The closed dep RESOLVES — that is what `epic ls --all` buys — but it must
	// not join the default population, which surfaces index as a picker.
	cd := b.Epic(closedDep)
	if cd == nil || cd.Closed.IsZero() {
		t.Errorf("%s is closed and must reach the snapshot WITH its stamp: %+v", closedDep, cd)
	}
	for _, x := range b.Epics() {
		if x.ID == closedDep {
			t.Errorf("%s is closed and must not reach Epics()", closedDep)
		}
	}
	if len(e.OpenDeps) != 1 || e.OpenDeps[0] != dep {
		t.Errorf("OpenDeps = %v, want [%s] — furrow resolves the closed dep away", e.OpenDeps, dep)
	}
}

func TestContractPersistDepAddAndRm(t *testing.T) {
	p, dir := newLabProvider(t)
	waiter := labAdd(t, dir, "待つ方")
	blocker := labAdd(t, dir, "先にやる方")

	if err := p.PersistDepAdd(waiter, blocker); err != nil {
		t.Fatalf("dep add: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := p.Board().Task(waiter); len(got.Deps) != 1 || got.Deps[0] != blocker {
		t.Errorf("deps = %v, want [%s]", got.Deps, blocker)
	}

	// The acyclic rule is furrow's own: the reverse edge must be refused,
	// which is what makes the board-side mirror safe to trust optimistically.
	if err := p.PersistDepAdd(blocker, waiter); err == nil {
		t.Error("the cycle-closing dep was accepted; furrow must refuse it")
	}
	if err := p.PersistDepAdd(waiter, "t-nope"); err == nil {
		t.Error("a dep on a missing id was accepted; every dep must exist")
	}

	if err := p.PersistDepRm(waiter, blocker); err != nil {
		t.Fatalf("dep rm: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := p.Board().Task(waiter); len(got.Deps) != 0 {
		t.Errorf("deps = %v, want none after the rm", got.Deps)
	}
}

// Refs and the note append ride the same PersistFields/PersistNote seams the
// overlay uses; read the store back and believe IT, not the patch. The note
// half also pins the join AppendNote mirrors (one blank line, one trailing
// newline) against furrow's own file — the mirror's measurement, kept honest.
//
// bite-exempt: pins furrow v5.1.0's current ref grammar against the real CLI
// (self-skips where furrow is absent, as on the bite runner); the mirror's own
// bite is TestSetFieldsRefsKeepCommaAndQuoteVerbatim in internal/board.
func TestContractRefEditsAndNoteAppend(t *testing.T) {
	p, dir := newLabProvider(t)
	id := labAdd(t, dir, "参照とメモの対象")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}

	// Adds keep the order given (refs are a sequence, not a sorted set).
	if err := p.PersistFields(id, board.FieldPatch{
		AddRefs: []string{"internal/x.go:12", "https://example.com/spec"},
	}); err != nil {
		t.Fatal(err)
	}
	// One mixed write: rm one, add three — furrow composes all in one call.
	// The comma'd URL and the quoted text pin furrow #317: --add/--rm are
	// pflag StringArrays, so both land as ONE ref each, verbatim (the CSV era
	// split the first and refused the second with exit 2).
	if err := p.PersistFields(id, board.FieldPatch{
		AddRefs: []string{
			"-dash.md:1", // a leading dash rides as a flag VALUE, never a positional
			"https://example.com/spec?rows=1,2",
			`say "hi"`,
		},
		RmRefs: []string{"internal/x.go:12"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	got := p.Board().Task(id)
	if want := []string{"https://example.com/spec", "-dash.md:1", "https://example.com/spec?rows=1,2", `say "hi"`}; strings.Join(got.Refs, "|") != strings.Join(want, "|") {
		t.Errorf("refs = %v, want %v", got.Refs, want)
	}
	// --rm is exact-match on the verbatim text, quote and all.
	if err := p.PersistFields(id, board.FieldPatch{RmRefs: []string{`say "hi"`}}); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	got = p.Board().Task(id)
	if want := []string{"https://example.com/spec", "-dash.md:1", "https://example.com/spec?rows=1,2"}; strings.Join(got.Refs, "|") != strings.Join(want, "|") {
		t.Errorf("refs after rm = %v, want %v", got.Refs, want)
	}

	// The note appends a paragraph AND advances updated, in one command. The
	// stamps are second-precision, so the advance is only observable across a
	// real second — hence the sleep; a same-instant write made the first
	// version of this assertion vacuously green (found by review).
	before := got.Updated
	wantBody := got.Body
	time.Sleep(1100 * time.Millisecond)
	if err := p.PersistNote(id, "-先頭ダッシュでも一段落として載る"); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	got = p.Board().Task(id)
	mirror := board.NewBoard([]*board.Task{{ID: id, Title: "x", Status: "inbox", Body: wantBody}})
	if err := mirror.AppendNote(id, "-先頭ダッシュでも一段落として載る"); err != nil {
		t.Fatal(err)
	}
	if want := mirror.Task(id).Body; got.Body != want {
		t.Errorf("body after note = %q, want the AppendNote mirror %q", got.Body, want)
	}
	if !got.Updated.After(before) {
		t.Errorf("updated did not advance across the note: %v (before %v)", got.Updated, before)
	}
}

// `furrow review <id>` stamps `reviewed` and leaves `updated` alone — the
// fact Board.Review mirrors. Second-precision stamps again, so the assertion
// only means something across a real second.
func TestContractReviewStampsReviewedNotUpdated(t *testing.T) {
	p, dir := newLabProvider(t)
	id := labAdd(t, dir, "見直し済みにする")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	before := *p.Board().Task(id)
	if !before.Reviewed.IsZero() {
		t.Fatalf("a fresh task must start unreviewed, got %v", before.Reviewed)
	}
	time.Sleep(1100 * time.Millisecond)
	if err := p.PersistReview(id); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	got := p.Board().Task(id)
	if got.Reviewed.IsZero() || !got.Reviewed.After(before.Updated) {
		t.Errorf("reviewed = %v, want a stamp after %v", got.Reviewed, before.Updated)
	}
	if !got.Updated.Equal(before.Updated) {
		t.Errorf("updated moved %v -> %v across a review; a review changes no content", before.Updated, got.Updated)
	}
	if err := p.PersistReview("t-nope"); err == nil {
		t.Error("reviewing a missing id was accepted")
	}
}

// `furrow revisit --json` rows decode into ids + reasons; -q narrows on
// furrow's side and a refused query comes back as an error with no rows.
func TestContractRevisitCarriesFurrowsReasons(t *testing.T) {
	p, dir := newLabProvider(t)
	bare := labAdd(t, dir, "見積り無し")
	sized := labAdd(t, dir, "見積り済み", "--value", "3", "--effort", "2", "-l", "sized")
	blocker := labAdd(t, dir, "先に終わる方", "--value", "1", "--effort", "1")
	lab(t, dir, "furrow", "done", blocker)
	waiter := labAdd(t, dir, "終わった dep を持つ方", "--value", "2", "--effort", "2", "--dep", blocker)

	rows, err := p.Revisit("")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string][]board.RevisitReason{}
	for _, r := range rows {
		by[r.ID] = r.Reasons
	}
	if rs := by[bare]; len(rs) != 2 || rs[0].Code != "value_unset" || rs[1].Code != "effort_unset" ||
		rs[0].Detail != "value estimate missing" {
		t.Errorf("%s reasons = %+v, want value_unset then effort_unset with furrow's detail", bare, rs)
	}
	if _, ok := by[sized]; ok {
		t.Errorf("%s is sized and fresh; it must not surface", sized)
	}
	if rs := by[waiter]; len(rs) != 1 || rs[0].Code != "dep_done" || rs[0].Detail != "dep "+blocker+" is done" {
		t.Errorf("%s reasons = %+v, want one dep_done naming %s", waiter, rs, blocker)
	}
	if _, ok := by[blocker]; ok {
		t.Errorf("%s is done; revisit lists open tasks", blocker)
	}
	// Every TERMINAL lane is skipped, not just done: the same estimate-less
	// task drops out of revisit the moment it is parked in icebox. memstore's
	// terminalLanes mirrors this set.
	lab(t, dir, "furrow", "move", bare, "icebox")
	rows, err = p.Revisit("")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.ID == bare {
			t.Errorf("%s is in icebox and still surfaced: %+v", bare, r.Reasons)
		}
	}

	narrowed, err := p.Revisit("label:sized")
	if err != nil {
		t.Fatal(err)
	}
	if len(narrowed) != 0 {
		t.Errorf("-q label:sized must narrow to nothing (the sized task carries no signal), got %+v", narrowed)
	}
	if rows, err := p.Revisit("value:>"); err == nil || rows != nil {
		t.Errorf("a half-typed query must be refused whole, got %d rows, err %v", len(rows), err)
	}
}

// furrow accepts a terminal lane in next.lanes and echoes it in the board JSON,
// but `furrow next` and `is:actionable` still skip it. ridge's Lane.Next must
// mirror what furrow DOES, not what the config says: a done task with its deps
// satisfied is not actionable, and the ▸ it would otherwise earn is a lie the
// board's own `v` sits beside.
func TestNextLanesExcludeTerminalLanesLikeFurrowDoes(t *testing.T) {
	p, dir := newLabProvider(t)
	lab(t, dir, "furrow", "config", "set", "next.lanes", "ready,in-progress,done")
	id := labAdd(t, dir, "closed while listed as next", "-s", "ready")
	lab(t, dir, "furrow", "set", id, "-s", "done")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	b := p.Board()
	if l := b.Lane("ready"); l == nil || !l.Next {
		t.Errorf("ready must stay a next lane, got %+v", l)
	}
	if l := b.Lane("done"); l == nil || l.Next {
		t.Errorf("done is terminal, so it is not a next lane whatever next.lanes says, got %+v", l)
	}
	if board.NewGraph(b).Actionable(id) {
		t.Errorf("%s is done; furrow next would not hand it out", id)
	}
	// The same answer furrow gives, read from the binary this test seeded.
	if out := lab(t, dir, "furrow", "ls", "-q", "is:actionable", "--json"); strings.TrimSpace(string(out)) != "[]" {
		t.Errorf("furrow is:actionable = %s, want [] — the premise of this test moved", out)
	}
}

// The same rule without the binary: a board JSON that lists done under
// next_lanes AND under terminal maps to a lane that is Done and not Next.
// (The contract test above proves furrow behaves this way; this one keeps
// the mapping pinned where furrow is not on PATH.)
func TestLanesFromDropsTerminalLanesFromNext(t *testing.T) {
	lanes := lanesFrom(boardJSON{
		Lanes:     []string{"inbox", "ready", "in-progress", "done", "icebox"},
		NextLanes: []string{"ready", "in-progress", "done"},
		Terminal:  []string{"done", "icebox"},
		DoneLane:  "done",
	})
	got := map[string]board.Lane{}
	for _, l := range lanes {
		got[l.Name] = l
	}
	if !got["ready"].Next || !got["in-progress"].Next {
		t.Errorf("ready / in-progress must be next lanes: %+v", lanes)
	}
	if got["done"].Next || !got["done"].Done {
		t.Errorf("done is terminal: Next must be false whatever next_lanes says, Done true — got %+v", got["done"])
	}
	if got["inbox"].Next || got["icebox"].Next {
		t.Errorf("lanes outside next_lanes stay non-next: %+v", lanes)
	}
	// Terminal is the board JSON's terminal set verbatim — the set the close
	// gate's OpenMembers reads, so it must be neither wider (done only) nor
	// narrower than what furrow's own IsTerminal answers.
	if !got["done"].Terminal || !got["icebox"].Terminal || got["ready"].Terminal || got["inbox"].Terminal || got["in-progress"].Terminal {
		t.Errorf("Terminal must mirror terminal exactly: %+v", lanes)
	}
}

// furrow #331 (board layout v10): a task carries a repeat rule, and closing
// it — by `done` or by `set -s <done>` — writes the next occurrence in the
// same write and answers with a `repeat` report. Both roads must surface the
// successor's id: it exists nowhere else until the re-read.
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
// on PATH — which is CI's build job, so the gate can never judge it there
func TestContractRepeatRidesTheLoadAndBothCloses(t *testing.T) {
	p, dir := newLabProvider(t)
	id := labAdd(t, dir, "週次の締め", "--due", "2026-10-01", "--repeat", "weekly")
	plain := labAdd(t, dir, "単発")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	x := p.Board().Task(id)
	if x.Repeat != "FREQ=WEEKLY" {
		t.Fatalf("repeat = %q, want furrow's compiled FREQ=WEEKLY", x.Repeat)
	}
	if x.RepeatAnchor.IsZero() || !x.RepeatAnchor.Equal(x.Due) {
		t.Errorf("repeat_anchor = %v, want the first due %v", x.RepeatAnchor, x.Due)
	}
	if y := p.Board().Task(plain); y.Repeat != "" || !y.RepeatAnchor.IsZero() {
		t.Errorf("a task with no rule read one: %q %v", y.Repeat, y.RepeatAnchor)
	}

	// Road one: `done`.
	rep, err := p.PersistDone(id)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Created == "" || rep.Due.IsZero() || rep.Completed {
		t.Fatalf("done answered no usable series report: %+v", rep)
	}
	if !rep.Due.After(x.Due) {
		t.Errorf("successor due %v is not after the settled %v", rep.Due, x.Due)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	succ := p.Board().Task(rep.Created)
	if succ == nil {
		t.Fatalf("the reported successor %s is not on the re-read board", rep.Created)
	}
	if succ.Repeat != "FREQ=WEEKLY" || !succ.Due.Equal(rep.Due) {
		t.Errorf("successor carries repeat=%q due=%v, want the rule and the reported due %v", succ.Repeat, succ.Due, rep.Due)
	}
	if prev := p.Board().Task(id); prev.Repeat != "" || prev.Status != p.Board().DoneLane() {
		t.Errorf("the closed occurrence must lose the rule and sit in done: %q %s", prev.Repeat, prev.Status)
	}

	// Road two: a placement into the done lane.
	mv, err := p.PersistMove(succ.ID, p.Board().DoneLane(), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if mv.Repeat == nil || mv.Repeat.Created == "" || mv.Repeat.Created == succ.ID || mv.Repeat.Completed {
		t.Fatalf("set -s done answered no usable series report: %+v", mv.Repeat)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if p.Board().Task(mv.Repeat.Created) == nil {
		t.Errorf("the successor %s reported by the move is not on the re-read board", mv.Repeat.Created)
	}

	// A spent series: the report says so instead of naming a successor.
	// COUNT=2, not 1 — furrow refuses a rule with no occurrence after the
	// first ("it would end the series on the very next close", exit 2), so
	// the shortest series that can be spent is two closes long.
	two := labAdd(t, dir, "二回きり", "--due", "2026-10-01", "--repeat", "FREQ=WEEKLY;COUNT=2")
	rep, err = p.PersistDone(two)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || rep.Created == "" || rep.Completed {
		t.Fatalf("the first close of a two-occurrence series must mint the second: %+v", rep)
	}
	rep, err = p.PersistDone(rep.Created)
	if err != nil {
		t.Fatal(err)
	}
	if rep == nil || !rep.Completed || rep.Created != "" || !rep.Due.IsZero() {
		t.Errorf("closing the last occurrence must report completed with no successor: %+v", rep)
	}
}

// The whole road through the program: `d` on a recurring task against the
// real store, and the status line the user reads must name the successor
// furrow wrote. Read through the -debuglog status layer — the final model's
// status is not reachable from this package, and the log is the same funnel.
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
// on PATH — which is CI's build job, so the gate can never judge it there
func TestContractProgramClosesARepeatingTaskForReal(t *testing.T) {
	p, dir := newLabProvider(t)
	id := labAdd(t, dir, "閉じる対象", "--due", "2026-10-01", "--repeat", "weekly")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}

	var log bytes.Buffer
	m := ui.New(p, ui.Options{Debug: ui.NewDebugLog(&log)})
	var in bytes.Buffer
	in.WriteString("d")
	in.WriteString("q") // waits for the drain (quitOrFlush), so the write lands first
	var out bytes.Buffer
	if _, err := tea.NewProgram(m,
		tea.WithInput(&in), tea.WithOutput(&out),
		tea.WithoutSignals(), tea.WithWindowSize(140, 40),
	).Run(); err != nil {
		t.Fatalf("program: %v", err)
	}

	// The store: the occurrence closed, its successor born in the default lane.
	if got := labLaneOrder(t, dir, "done"); len(got) != 1 || got[0] != id {
		t.Fatalf("store done = %v, want [%s]", got, id)
	}
	inbox := labLaneOrder(t, dir, "inbox")
	if len(inbox) != 1 {
		t.Fatalf("store inbox = %v, want the one successor", inbox)
	}
	// The screen: the status note named it.
	var announced bool
	for _, line := range strings.Split(strings.TrimSpace(log.String()), "\n") {
		var ev struct {
			Layer, Kind, Text string
		}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("debug log line %q: %v", line, err)
		}
		if ev.Layer == "status" && ev.Kind == "note" && strings.Contains(ev.Text, "repeat: next due") && strings.Contains(ev.Text, "("+inbox[0]+")") {
			announced = true
		}
	}
	if !announced {
		t.Errorf("no status note named the successor %s; log:\n%s", inbox[0], log.String())
	}
}

// zoneOf mirrors furrow's own reading of [due].timezone: a loadable IANA name
// is the calendar; "" (none declared) and a name the host cannot load are the
// process zone (config.go warns "using the process zone" for the latter).
func TestZoneOfLoadsAnIANANameAndFallsBackToTheProcessZone(t *testing.T) {
	if got := zoneOf(""); got != nil {
		t.Errorf("zoneOf(\"\") = %v, want nil (the process zone)", got)
	}
	if got := zoneOf("Nowhere/Nope"); got != nil {
		t.Errorf("zoneOf(unloadable) = %v, want nil (the process zone)", got)
	}
	if got := zoneOf("Asia/Tokyo"); got == nil || got.String() != "Asia/Tokyo" {
		t.Errorf("zoneOf(Asia/Tokyo) = %v, want the loaded zone", got)
	}
}

// The calendar chain against the real CLI (furrow #330): a board declaring
// [due].timezone binds a bare --due at that calendar's last second, and the
// reload declares that calendar (board.SetZone) — under no test pin, Zone()
// IS it — so the day ridge renders is the day furrow prints.
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
// on PATH — which is CI, so the gate can never judge it there
func TestContractBoardCalendarComesFromDueTimezone(t *testing.T) {
	p, dir := newLabProvider(t)
	t.Cleanup(board.SetClock(nil, func() *time.Location { return nil }))
	t.Cleanup(func() { board.SetZone(nil) })
	lab(t, dir, "furrow", "config", "set", "due.timezone", "Asia/Tokyo")
	id := labAdd(t, dir, "盤の暦で締切", "--due", "2026-10-02")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := board.Zone().String(); got != "Asia/Tokyo" {
		t.Fatalf("Zone() = %s after the reload, want the board's Asia/Tokyo — the load must declare it", got)
	}
	tk := p.Board().Task(id)
	if tk == nil {
		t.Fatal("the added task must be on the reloaded board")
	}
	if want := time.Date(2026, 10, 2, 14, 59, 59, 0, time.UTC); !tk.Due.Equal(want) {
		t.Errorf("furrow bound the bare day at %s, want %s (the last second of 2026-10-02 in Asia/Tokyo)", tk.Due.Format(time.RFC3339), want.Format(time.RFC3339))
	}
	if got := tk.Due.In(board.Zone()).Format("2006-01-02"); got != "2026-10-02" {
		t.Errorf("rendered day = %s, want 2026-10-02 — in Auckland that instant is the 3rd", got)
	}
}

// `sync --json` progress, decoded: complete, the two body lists (omitted
// when empty — nil here, and syncNote reads len) and the stash count.
func TestSyncProgressDecodesIntoTheReport(t *testing.T) {
	var prog syncProgressJSON
	raw := `{"committed":true,"pulled":true,"pushed":true,"conflict":false,"complete":false,"pending_bodies":["t-a"],"committed_bodies":["t-b","t-c"],"pending_stash":[{"ref":"stash@{0}","paths":["stray.txt"]}]}`
	if err := json.Unmarshal([]byte(raw), &prog); err != nil {
		t.Fatal(err)
	}
	got := prog.report()
	if got.Complete || !slices.Equal(got.Committed, []string{"t-b", "t-c"}) || !slices.Equal(got.Pending, []string{"t-a"}) || got.Stash != 1 {
		t.Errorf("report = %+v", got)
	}
	prog = syncProgressJSON{}
	if err := json.Unmarshal([]byte(`{"committed":false,"pulled":true,"pushed":true,"conflict":false,"complete":true}`), &prog); err != nil {
		t.Fatal(err)
	}
	if got := prog.report(); !got.Complete || len(got.Committed) != 0 || len(got.Pending) != 0 {
		t.Errorf("report = %+v, want complete and nothing named", got)
	}
}

// The publish chain against the real CLI, on a lab with a bare remote: a body
// ridge rewrote is a modified file a bare `furrow sync` leaves for its author
// (Store.Sync's doc; measured on dev 0f7559d: pending_bodies [id], complete
// false), so the adapter names it with -b and the checkout is clean and
// pushed after the sync; a hand edit is not ridge's to name and is reported
// pending instead.
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
// on PATH — which is CI, so the gate can never judge it there
func TestContractSyncPublishesTheBodiesRidgeWrote(t *testing.T) {
	p, dir := newLabProvider(t)
	remote := t.TempDir()
	lab(t, dir, "git", "init", "-q", "--bare", remote)
	lab(t, dir, "git", "config", "user.email", "lab@example.com")
	lab(t, dir, "git", "config", "user.name", "lab")
	lab(t, dir, "git", "remote", "add", "origin", remote)
	lab(t, dir, "git", "add", "-A")
	lab(t, dir, "git", "commit", "-q", "-m", "init")
	lab(t, dir, "git", "push", "-q", "-u", "origin", "HEAD")
	clean := func(when string) {
		t.Helper()
		if out := lab(t, dir, "git", "status", "--porcelain"); strings.TrimSpace(string(out)) != "" {
			t.Errorf("%s: the checkout must be clean, got:\n%s", when, out)
		}
		if out := lab(t, dir, "git", "status", "-sb"); strings.Contains(string(out), "ahead") {
			t.Errorf("%s: the sync must have pushed, got %s", when, strings.SplitN(string(out), "\n", 2)[0])
		}
	}

	id := labAdd(t, dir, "本文を書き直す一枚")
	// The seed body is NEW to git, which a bare sync commits on its own.
	rep, err := p.Sync()
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if !rep.Complete {
		t.Fatalf("first sync must be complete, got %+v", rep)
	}
	clean("after the first sync")

	if err := p.PersistBody(id, "ridge が書いた本文\n"); err != nil {
		t.Fatal(err)
	}
	rep, err = p.Sync()
	if err != nil {
		t.Fatalf("sync after PersistBody: %v", err)
	}
	if !rep.Complete || !slices.Contains(rep.Committed, id) || len(rep.Pending) != 0 {
		t.Errorf("a body ridge wrote must be committed by the sync that follows, got %+v", rep)
	}
	clean("after the sync that named ridge's body")
	if _, still := p.dirty[id]; still {
		t.Errorf("a committed body must leave the dirty set")
	}

	// A hand edit on the checkout: not ridge's, so not named — reported.
	if err := os.WriteFile(filepath.Join(dir, ".furrow", "bodies", id+".md"), []byte("手で直した本文\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	rep, err = p.Sync()
	if err != nil {
		t.Fatalf("sync over a hand edit: %v", err)
	}
	if rep.Complete || !slices.Contains(rep.Pending, id) {
		t.Errorf("a hand-edited body must be reported pending, got %+v", rep)
	}
	// Rewritten by ridge, it is ridge's again and goes out.
	if err := p.PersistBody(id, "ridge が書き直した本文\n"); err != nil {
		t.Fatal(err)
	}
	rep, err = p.Sync()
	if err != nil {
		t.Fatalf("sync after the rewrite: %v", err)
	}
	if !rep.Complete || !slices.Contains(rep.Committed, id) {
		t.Errorf("the rewrite must be committed, got %+v", rep)
	}
	clean("after the rewrite's sync")
}

// The bookkeeping without a binary: the argv names every dirty body, sorted;
// a body rewritten while the sync ran stays named after the sync — its
// committed_bodies entry is the write furrow saw at the START, so the later
// write is not in that commit (found by review, reproduced against a real
// furrow with a write 1.5 s into a 3 s sync: the bare delete lost it).
func TestSyncNamesEveryDirtyBodyAndKeepsOneRewrittenWhileItRan(t *testing.T) {
	p := &Store{}
	p.noteDirty("t-b")
	p.noteDirty("t-a")
	named := p.dirtySnapshot()
	if got := syncArgs(named); !slices.Equal(got, []string{"sync", "--json", "-b", "t-a", "-b", "t-b"}) {
		t.Errorf("argv = %v", got)
	}
	p.noteDirty("t-a") // rewritten mid-sync
	p.forgetCommitted(named, []string{"t-a", "t-b"})
	if _, still := p.dirty["t-a"]; !still {
		t.Error("t-a was rewritten after the snapshot and must stay named for the next sync")
	}
	if _, still := p.dirty["t-b"]; still {
		t.Error("t-b was committed as named and must be forgotten")
	}
	if got := syncArgs(p.dirtySnapshot()); !slices.Equal(got, []string{"sync", "--json", "-b", "t-a"}) {
		t.Errorf("next argv = %v", got)
	}
}

func sp(s string) *string { return &s }

// The edit menu's repeat row and quick add's repeat: token (t-zbmv) are
// `furrow set --repeat` / `--clear-repeat` and `add --repeat`: the argv
// spellings and furrow's coupling of the rule to the due and to the closed
// stamp are the contract, run here against the real binary (the CI contract
// job runs it on the pinned release). The three refusals ride the envelope
// as kind validation, which is how the UI's rollback names them.
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
// on PATH — which is CI's build job, so the gate can never judge it there
func TestContractRepeatEditsAreSetRepeatAndClearRepeat(t *testing.T) {
	p, dir := newLabProvider(t)
	id := labAdd(t, dir, "締めの確認", "--due", "2026-10-02")
	bare := labAdd(t, dir, "due なし")
	closed := labAdd(t, dir, "閉じた", "--due", "2026-10-02")
	lab(t, dir, "furrow", "done", closed)
	reload := func() *board.Task {
		t.Helper()
		if err := p.Reload(); err != nil {
			t.Fatal(err)
		}
		return p.Board().Task(id)
	}
	firstDue := reload().Due

	if err := p.PersistFields(id, board.FieldPatch{Repeat: sp("every 2 weeks on mon,thu")}); err != nil {
		t.Fatalf("set --repeat: %v", err)
	}
	x := reload()
	if x.Repeat != "FREQ=WEEKLY;INTERVAL=2;BYDAY=MO,TH" || !x.RepeatAnchor.Equal(firstDue) {
		t.Fatalf("after set --repeat: repeat=%q anchor=%v, want the compiled rule anchored at the due %v", x.Repeat, x.RepeatAnchor, firstDue)
	}

	// A due alone moves this occurrence; the anchor stays.
	if err := p.PersistFields(id, board.FieldPatch{Due: sp("2026-10-09")}); err != nil {
		t.Fatal(err)
	}
	x = reload()
	if x.Due.Equal(firstDue) || !x.RepeatAnchor.Equal(firstDue) {
		t.Errorf("after set --due: due=%v anchor=%v, want a moved due and the anchor at %v", x.Due, x.RepeatAnchor, firstDue)
	}

	// The seed round trip: the compiled rule read back as a raw RRULE is
	// never refused, and re-anchors at the due now carried.
	if err := p.PersistFields(id, board.FieldPatch{Repeat: sp(x.Repeat)}); err != nil {
		t.Fatalf("set --repeat with the compiled rule: %v", err)
	}
	y := reload()
	if y.Repeat != x.Repeat || !y.RepeatAnchor.Equal(x.Due) {
		t.Errorf("after re-committing the rule: repeat=%q anchor=%v, want the same rule anchored at %v", y.Repeat, y.RepeatAnchor, x.Due)
	}

	// The three refusals, as the envelope names them — and the drop that is
	// not one: a closed task takes no rule but sheds one at exit 0.
	wantKind(t, p.PersistFields(id, board.FieldPatch{Due: sp("")}), "validation")
	wantKind(t, p.PersistFields(bare, board.FieldPatch{Repeat: sp("weekly")}), "validation")
	wantKind(t, p.PersistFields(closed, board.FieldPatch{Repeat: sp("weekly")}), "validation")
	if err := p.PersistFields(closed, board.FieldPatch{Repeat: sp("")}); err != nil {
		t.Errorf("--clear-repeat on a closed task: %v, want exit 0", err)
	}

	if err := p.PersistFields(id, board.FieldPatch{Repeat: sp("")}); err != nil {
		t.Fatalf("set --clear-repeat: %v", err)
	}
	if z := reload(); z.Repeat != "" || !z.RepeatAnchor.IsZero() || z.Due.IsZero() {
		t.Errorf("after --clear-repeat: %+v, want the rule and anchor gone and the due kept", z)
	}
	// Dropping a rule the task no longer has is exit 0 (changed: []).
	if err := p.PersistFields(id, board.FieldPatch{Repeat: sp("")}); err != nil {
		t.Errorf("a second --clear-repeat: %v, want exit 0", err)
	}

	// add --repeat rides beside --due; the compiled form is on the re-read.
	added, err := p.Add("週次の締め", board.AddOptions{Repo: "lab/lab", Due: "2026-10-02", Repeat: "weekly on fri"})
	if err != nil {
		t.Fatalf("add --repeat: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	got := p.Board().Task(added)
	if got == nil || !strings.HasPrefix(got.Repeat, "FREQ=WEEKLY") || !strings.Contains(got.Repeat, "BYDAY=FR") || !got.RepeatAnchor.Equal(got.Due) {
		t.Errorf("add --repeat landed as %+v, want a weekly-on-Friday rule anchored at the due", got)
	}
}

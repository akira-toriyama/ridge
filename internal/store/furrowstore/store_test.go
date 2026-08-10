package furrowstore

import (
	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/ui"

	"bytes"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
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
	if got := b.Lane(b.DoneLane()); got == nil || !got.Term {
		t.Error("the done lane must be terminal")
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
	renumbered, err := p.PersistMove(c, "ready", a, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(renumbered) == 0 {
		t.Error("an exhausted gap must report renumbered neighbours")
	}
}

func TestContractPersistDoneCheckBody(t *testing.T) {
	p, dir := newLabProvider(t)
	id := labAdd(t, dir, "仕上げ対象", "--check", "項目")

	if err := p.PersistDone(id); err != nil {
		t.Fatal(err)
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

// The full loop, for real: a live Program over a real store, a keyboard
// gesture, and the store itself as the assertion target. This is the one test
// where the optimistic queue's Cmd actually runs inside bubbletea's loop and
// the reconcile re-read lands.
func TestContractProgramMovesForReal(t *testing.T) {
	p, dir := newLabProvider(t)
	id := labAdd(t, dir, "動かす対象")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}

	m := ui.New(p, ui.Options{})
	var in bytes.Buffer
	in.WriteString("]") // cycle the selected card one lane forward
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
	if got.Due.Format("2006-01-02") != "2026-09-01" {
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

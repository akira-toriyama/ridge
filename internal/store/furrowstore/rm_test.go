package furrowstore

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
)

// `rm` against the real CLI (furrow #326, in the v6.0.0 pin): the plain dry
// run of a referenced target is refused exactly like the apply (kind
// referenced — measured, which is why the gate reads its preview with
// --force); the --force dry run names the targets as they are (the rule a
// recurring one carries included) and what points at them; the apply is
// refused while anything does unless --force, which drops the dep edge and
// de-links the [[id]] to the bare id; a miss is all-or-nothing. ridge copies
// the report and re-derives none of it.
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
// on PATH — which is CI's bite job, so the gate can never judge it there
func TestContractRemovePreviewsRefusesAndSevers(t *testing.T) {
	p, dir := newLabProvider(t)
	target := labAdd(t, dir, "取り下げる一枚", "--due", "2027-03-31", "--repeat", "weekly")
	waiter := labAdd(t, dir, "待つ一枚")
	lab(t, dir, "furrow", "dep", waiter, target)
	linker := labAdd(t, dir, "言及する一枚", "--body", "see [["+target+"]] first")
	alone := labAdd(t, dir, "誰も指さない一枚")

	if _, err := p.Remove([]string{target}, board.RemoveOptions{}); err == nil || !strings.Contains(err.Error(), "referenced") {
		t.Fatalf("the plain dry run of a referenced target must be refused with furrow's kind, got %v", err)
	}
	rep, err := p.Remove([]string{target}, board.RemoveOptions{Force: true})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !rep.DryRun || !rep.Force || len(rep.Tasks) != 1 || rep.Tasks[0].ID != target {
		t.Fatalf("preview = %+v, want the --force dry run naming %s", rep, target)
	}
	if rep.Tasks[0].Repeat == "" || rep.Tasks[0].Due.IsZero() {
		t.Errorf("the target carries a rule and a due; the report must say so: %+v", rep.Tasks[0])
	}
	if !slices.Equal(rep.References.Deps, []board.RefEdge{{From: waiter, To: target}}) {
		t.Errorf("deps = %+v, want the one edge from %s", rep.References.Deps, waiter)
	}
	if !slices.Equal(rep.References.Links, []board.RefLink{{Body: linker, To: target}}) {
		t.Errorf("links = %+v, want the one [[link]] in %s", rep.References.Links, linker)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if p.Board().Task(target) == nil {
		t.Fatal("a preview must remove nothing")
	}

	if _, err := p.Remove([]string{target}, board.RemoveOptions{Apply: true}); err == nil || !strings.Contains(err.Error(), "referenced") {
		t.Fatalf("a referenced target must be refused with furrow's kind, got %v", err)
	}
	rep, err = p.Remove([]string{target}, board.RemoveOptions{Force: true, Apply: true})
	if err != nil {
		t.Fatalf("forced apply: %v", err)
	}
	if rep.DryRun || !rep.Force || len(rep.References.Deps) != 1 || len(rep.References.Links) != 1 {
		t.Errorf("the apply report must say force and what it severed: %+v", rep)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	b := p.Board()
	if b.Task(target) != nil {
		t.Error("the target is still on the board")
	}
	if w := b.Task(waiter); w == nil || len(w.Deps) != 0 {
		t.Errorf("--force must drop the dep edge: %+v", w)
	}
	if l := b.Task(linker); l == nil || strings.Contains(l.Body, "[["+target+"]]") || !strings.Contains(l.Body, target) {
		t.Errorf("--force must de-link [[%s]] to the bare id, keeping the words: %+v", target, l)
	}

	// Duplicates collapse (furrow's rule and the adapter's): one target, once.
	if rep, err := p.Remove([]string{alone, alone}, board.RemoveOptions{}); err != nil || len(rep.Tasks) != 1 {
		t.Errorf("a duplicated id must preview as one target: %v %+v", err, rep)
	}
	rep, err = p.Remove([]string{alone}, board.RemoveOptions{Apply: true})
	if err != nil || !rep.References.Empty() {
		t.Fatalf("an unreferenced target deletes without --force: %v %+v", err, rep)
	}
	if _, err := p.Remove([]string{waiter, "t-nope"}, board.RemoveOptions{Apply: true}); err == nil {
		t.Error("a miss must refuse the whole list")
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if p.Board().Task(alone) != nil || p.Board().Task(waiter) == nil {
		t.Error("the plain delete must land and the missed list must move nothing")
	}
}

// `epic rm` against the real CLI: a member and a box's dep on the target are
// its references; --force unfiles the member, drops the edge, and the box is
// gone from the --all read.
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
// on PATH — which is CI's bite job, so the gate can never judge it there
func TestContractEpicRemoveUnfilesMembersUnderForce(t *testing.T) {
	p, dir := newLabProvider(t)
	box := labEpic(t, dir, "取り下げる箱", "lab/lab")
	member := labAdd(t, dir, "会員の一枚", "-e", box)
	other := labEpic(t, dir, "後で開く箱", "lab/lab")
	lab(t, dir, "furrow", "epic", "dep", other, "--", box)

	if _, err := p.EpicRemove(box, board.RemoveOptions{}); err == nil || !strings.Contains(err.Error(), "referenced") {
		t.Fatalf("the plain dry run of a referenced box must be refused with furrow's kind, got %v", err)
	}
	rep, err := p.EpicRemove(box, board.RemoveOptions{Force: true})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if !rep.DryRun || !rep.Force || rep.Epic == nil || rep.Epic.ID != box {
		t.Fatalf("preview = %+v, want the --force dry run naming %s", rep, box)
	}
	if !slices.Equal(rep.References.Members, []board.RefMember{{Task: member, Epic: box}}) {
		t.Errorf("members = %+v, want %s", rep.References.Members, member)
	}
	if !slices.Equal(rep.References.EpicDeps, []board.RefEdge{{From: other, To: box}}) {
		t.Errorf("epic deps = %+v, want the edge from %s", rep.References.EpicDeps, other)
	}
	if _, err := p.EpicRemove(box, board.RemoveOptions{Apply: true}); err == nil || !strings.Contains(err.Error(), "referenced") {
		t.Fatalf("a referenced box must be refused with furrow's kind, got %v", err)
	}
	if _, err := p.EpicRemove(box, board.RemoveOptions{Force: true, Apply: true}); err != nil {
		t.Fatalf("forced apply: %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	b := p.Board()
	if b.Epic(box) != nil {
		t.Error("the box is still served")
	}
	if m := b.Task(member); m == nil || m.Epic != "" {
		t.Errorf("--force must unfile the member: %+v", m)
	}
	if o := b.Epic(other); o == nil || slices.Contains(o.Deps, box) {
		t.Errorf("--force must drop the other box's dep on it: %+v", o)
	}
}

// The asset transfer against a furrow that has it (furrow #345 — newer than
// the v6.0.0 pin, so this skips there): an attached image goes with its
// task, and one another body still shows is kept and says who holds it. The
// binary is told apart by the report shape its `rm -h` prints — v6.0.0's is
// `{dry_run, force, tasks, references}`, dev's ends in `references, assets}`
// — because the report itself cannot: an absent key and an empty transfer
// decode alike (CI ran this once against the pin, unskipped, on the word
// "assets" alone, which the prose of both helps carries).
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
// on PATH — which is CI's bite job, so the gate can never judge it there
func TestContractRemoveReportsTheAssetTransfer(t *testing.T) {
	p, dir := newLabProvider(t)
	help := strings.Join(strings.Fields(string(lab(t, dir, "furrow", "rm", "-h"))), " ")
	if !strings.Contains(help, "references, assets}") {
		t.Skip("this furrow's rm report carries no asset transfer (#345 is newer than v6.0.0)")
	}
	png := filepath.Join(t.TempDir(), "shot.png")
	if err := os.WriteFile(png, []byte("\x89PNG\r\n\x1a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	owner := labAdd(t, dir, "画像つきの一枚")
	lab(t, dir, "furrow", "attach", owner, png)
	rep, err := p.Remove([]string{owner}, board.RemoveOptions{Force: true})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if len(rep.Assets.Deleted) != 1 || len(rep.Assets.Kept) != 0 {
		t.Errorf("an asset only its owner shows goes with it: %+v", rep.Assets)
	}
	rep, err = p.Remove([]string{owner}, board.RemoveOptions{Force: true, Apply: true})
	if err != nil || len(rep.Assets.Deleted) != 1 {
		t.Errorf("the apply reports the same transfer: %v %+v", err, rep.Assets)
	}
}

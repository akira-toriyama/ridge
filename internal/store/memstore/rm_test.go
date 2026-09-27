package memstore

import (
	"slices"
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The preview over the fixture lists what furrow would: t-t38k is the
// fixture's most-referenced task — dep edges from t-jv3j and others, and
// [[t-t38k]] in several bodies — and a preview changes nothing. Read with
// Force, as the gate reads it: the plain dry run of a referenced target is
// refused, the way furrow refuses it.
func TestRemovePreviewListsWhatPointsAtTheTarget(t *testing.T) {
	p := New()
	before := len(p.Board().Tasks())
	if _, err := p.Remove([]string{"t-t38k"}, board.RemoveOptions{}); err == nil || !strings.Contains(err.Error(), "still referenced") {
		t.Fatalf("the plain dry run of a referenced target is refused like the apply, got %v", err)
	}
	rep, err := p.Remove([]string{"t-t38k"}, board.RemoveOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.DryRun || !rep.Force {
		t.Errorf("dry_run=%t force=%t, want the --force dry run", rep.DryRun, rep.Force)
	}
	if len(rep.Tasks) != 1 || rep.Tasks[0].ID != "t-t38k" || rep.Tasks[0].Title == "" {
		t.Errorf("tasks = %+v, want t-t38k as it is", rep.Tasks)
	}
	from := map[string]bool{}
	for _, d := range rep.References.Deps {
		if d.To != "t-t38k" {
			t.Errorf("dep edge %+v names another target", d)
		}
		from[d.From] = true
	}
	if !from["t-jv3j"] {
		t.Errorf("t-jv3j waits on t-t38k and must be a dep reference: %+v", rep.References.Deps)
	}
	if len(rep.References.Links) == 0 {
		t.Error("the fixture's bodies link [[t-t38k]]; the preview must list them")
	}
	for _, l := range rep.References.Links {
		owner := p.Board().Task(l.Body)
		if l.To != "t-t38k" || owner == nil || !strings.Contains(owner.Body, "[[t-t38k]]") {
			t.Errorf("link %+v is not a live [[t-t38k]] in that body", l)
		}
	}
	if len(rep.References.Members)+len(rep.References.EpicDeps) != 0 {
		t.Errorf("a task target has no members or epic deps: %+v", rep.References)
	}
	if got := len(p.Board().Tasks()); got != before || p.Board().Task("t-t38k") == nil {
		t.Error("a preview must change nothing")
	}
}

// The refusal and the sever: without --force a referenced target is refused
// in furrow's words and nothing moves; with it the target goes, the dep edges
// naming it are dropped, and [[t-t38k]] in every body reads as the bare id —
// and all of that survives Reload, as an archive does.
func TestRemoveRefusesWhileReferencedAndForceSevers(t *testing.T) {
	p := New()
	_, err := p.Remove([]string{"t-t38k"}, board.RemoveOptions{Apply: true})
	if err == nil || !strings.Contains(err.Error(), "still referenced") || !strings.Contains(err.Error(), "--force") {
		t.Fatalf("a referenced target must be refused in furrow's words, got %v", err)
	}
	if p.Board().Task("t-t38k") == nil {
		t.Fatal("a refusal must remove nothing")
	}
	rep, err := p.Remove([]string{"t-t38k"}, board.RemoveOptions{Force: true, Apply: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.DryRun || !rep.Force || rep.References.Empty() {
		t.Errorf("the apply report must say force and list what it severed: %+v", rep)
	}
	check := func(when string) {
		t.Helper()
		b := p.Board()
		if b.Task("t-t38k") != nil {
			t.Errorf("%s: t-t38k is still on the board", when)
		}
		for _, tk := range b.Tasks() {
			if slices.Contains(tk.Deps, "t-t38k") {
				t.Errorf("%s: %s still waits on the removed t-t38k", when, tk.ID)
			}
			if strings.Contains(tk.Body, "[[t-t38k]]") {
				t.Errorf("%s: %s's body still links [[t-t38k]]", when, tk.ID)
			}
		}
		if jv := b.Task("t-jv3j"); jv == nil || !strings.Contains(jv.Body, "t-t38k") {
			t.Errorf("%s: de-linking keeps the words — t-jv3j's body must still name t-t38k bare", when)
		}
	}
	check("after the write")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	check("after Reload")
}

// A box: its members are its references, and so is a box's dep on it;
// --force unfiles the members, drops the edge, and the box leaves the board.
func TestEpicRemoveUnfilesMembersUnderForce(t *testing.T) {
	p := New()
	rep, err := p.EpicRemove("e-fw2m", board.RemoveOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Epic == nil || rep.Epic.ID != "e-fw2m" || rep.Epic.Title == "" || !rep.DryRun {
		t.Fatalf("preview = %+v, want e-fw2m as it is, dry", rep)
	}
	if n := len(rep.References.Members); n != 18 {
		t.Errorf("e-fw2m has 18 members; the preview lists %d", n)
	}
	if len(rep.References.EpicDeps) != 1 || rep.References.EpicDeps[0] != (board.RefEdge{From: "e-c4mt", To: "e-fw2m"}) {
		t.Errorf("e-c4mt waits on e-fw2m; epic deps = %+v", rep.References.EpicDeps)
	}
	if _, err := p.EpicRemove("e-fw2m", board.RemoveOptions{Apply: true}); err == nil || !strings.Contains(err.Error(), "still referenced") {
		t.Fatalf("a referenced box must be refused, got %v", err)
	}
	if _, err := p.EpicRemove("e-fw2m", board.RemoveOptions{Force: true, Apply: true}); err != nil {
		t.Fatal(err)
	}
	check := func(when string) {
		t.Helper()
		b := p.Board()
		if b.Epic("e-fw2m") != nil {
			t.Errorf("%s: the box is still served", when)
		}
		for _, tk := range b.Tasks() {
			if tk.Epic == "e-fw2m" {
				t.Errorf("%s: %s is still filed under the removed box", when, tk.ID)
			}
		}
		if c := b.Epic("e-c4mt"); c == nil || slices.Contains(c.Deps, "e-fw2m") || slices.Contains(c.OpenDeps, "e-fw2m") {
			t.Errorf("%s: e-c4mt still names the removed box: %+v", when, c)
		}
	}
	check("after the write")
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	check("after Reload")
}

// All-or-nothing on a miss, the series disclosure on a recurring target, and
// the empty list refused before it reaches the store.
func TestRemoveIsAllOrNothingAndDisclosesASeries(t *testing.T) {
	p := New()
	if _, err := p.Remove([]string{"t-7wdg", "t-nope"}, board.RemoveOptions{Apply: true}); err == nil || !strings.Contains(err.Error(), "t-nope") {
		t.Fatalf("a miss must refuse the whole list and name it, got %v", err)
	}
	if p.Board().Task("t-7wdg") == nil {
		t.Error("a miss must remove nothing")
	}
	rep, err := p.Remove([]string{"t-9sa6"}, board.RemoveOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Tasks[0].Repeat == "" || rep.Tasks[0].Due.IsZero() {
		t.Errorf("t-9sa6 carries a rule and a due; the preview must say so: %+v", rep.Tasks[0])
	}
	if _, err := p.Remove(nil, board.RemoveOptions{}); err == nil {
		t.Error("an empty id list is refused before it reaches the store")
	}
	if _, err := p.EpicRemove("e-nope", board.RemoveOptions{}); err == nil {
		t.Error("an unknown box is refused")
	}
}

// The gated fixture refuses the write the way a schema-behind store does.
func TestRemoveOnAGatedBoardRefusesTheWrite(t *testing.T) {
	p := NewGated("board-behind")
	if _, err := p.Remove([]string{"t-7wdg"}, board.RemoveOptions{Apply: true}); err == nil {
		t.Error("the gated fixture must refuse the write")
	}
	if _, err := p.EpicRemove("e-7q1m", board.RemoveOptions{Apply: true}); err == nil {
		t.Error("the gated fixture must refuse the epic write")
	}
}

package memstore

import (
	"strings"
	"testing"
	"time"

	"github.com/akira-toriyama/ridge/internal/board"
)

func sweepBoard() *board.Board {
	old := time.Now().Add(-60 * 24 * time.Hour)
	fresh := time.Now().Add(-2 * 24 * time.Hour)
	return board.NewBoard([]*board.Task{
		{ID: "d-old", Title: "old done", Status: "done", Closed: old},
		{ID: "d-new", Title: "fresh done", Status: "done", Closed: fresh},
		{ID: "o-1", Title: "open waits on old", Status: "ready", Deps: []string{"d-old", "o-2"}},
		{ID: "o-2", Title: "open", Status: "backlog"},
	})
}

// The previews mirror furrow's rules: only done tasks past the age guard are
// archivable, only OPEN tasks' edges to done tasks are satisfied, and the
// archive starts empty.
func TestSweepPreviewMirrorsFurrowsRules(t *testing.T) {
	p := NewWith(sweepBoard())
	s, err := p.SweepPreview()
	if err != nil {
		t.Fatal(err)
	}
	if s.OlderThanDays != fixtureOlderThanDays {
		t.Errorf("older_than = %d", s.OlderThanDays)
	}
	if len(s.Archivable) != 1 || s.Archivable[0].ID != "d-old" {
		t.Errorf("archivable = %+v, want d-old alone (d-new is inside the guard)", s.Archivable)
	}
	if len(s.DoneDeps) != 1 || s.DoneDeps[0].ID != "o-1" || strings.Join(s.DoneDeps[0].Deps, ",") != "d-old" {
		t.Errorf("done deps = %+v, want o-1 → d-old only (o-2 is open)", s.DoneDeps)
	}
	if len(s.UnknownKeys) != 0 || len(s.Archived) != 0 {
		t.Errorf("unknown/archived = %d/%d, want empty", len(s.UnknownKeys), len(s.Archived))
	}
}

// Archive / Unarchive round-trip through a Reload: the write survives it (the
// quick-add rule), the refusals are furrow's, and nothing moves on a refused
// list.
func TestSweepArchiveRoundTripSurvivesReload(t *testing.T) {
	p := NewWith(sweepBoard())
	for _, bad := range [][]string{nil, {"o-1"}, {"d-old", "nope"}} {
		if err := p.Archive(bad); err == nil {
			t.Errorf("Archive(%v) must refuse", bad)
		}
	}
	if p.Board().Task("d-old") == nil {
		t.Fatal("a refused list must move nothing")
	}
	if err := p.Archive([]string{"d-old", "d-new"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if p.Board().Task("d-old") != nil || p.Board().Task("d-new") != nil {
		t.Error("archived tasks came back on Reload")
	}
	s, _ := p.SweepPreview()
	if len(s.Archived) != 2 || s.Archived[0].ID != "d-new" {
		t.Errorf("archived = %+v, want both, newest closed first", s.Archived)
	}
	if len(s.Archivable) != 0 {
		t.Errorf("archivable after the write = %+v", s.Archivable)
	}
	if err := p.Unarchive([]string{"o-1"}); err == nil || !strings.Contains(err.Error(), "already on the hot board") {
		t.Errorf("unarchive of a hot task = %v", err)
	}
	if err := p.Unarchive([]string{"d-old", "ghost"}); err == nil || !strings.Contains(err.Error(), "nothing was restored") {
		t.Errorf("unarchive with a miss = %v", err)
	}
	if p.Board().Task("d-old") != nil {
		t.Fatal("an all-or-nothing miss restored something")
	}
	if err := p.Unarchive([]string{"d-old"}); err != nil {
		t.Fatal(err)
	}
	if tk := p.Board().Task("d-old"); tk == nil || tk.Status != "done" || tk.Closed.IsZero() {
		t.Errorf("restored task = %+v, want back in done with its stamp", tk)
	}
}

// Tidy done-deps drops every satisfied edge and keeps it dropped across Reload;
// the open→open edge stays.
func TestSweepTidyDoneDepsSurvivesReload(t *testing.T) {
	p := NewWith(sweepBoard())
	if err := p.Tidy(board.TidyDoneDeps); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(p.Board().Task("o-1").Deps, ","); got != "o-2" {
		t.Errorf("o-1 deps = %q, want the open edge alone", got)
	}
	s, _ := p.SweepPreview()
	if len(s.DoneDeps) != 0 {
		t.Errorf("preview still lists %+v", s.DoneDeps)
	}
	if err := p.Tidy(board.TidyUnknownKeys); err != nil {
		t.Errorf("unknown-keys on a fixture with none must be a no-op, got %v", err)
	}
	if err := p.Tidy(board.TidyClass(9)); err == nil {
		t.Error("an unknown class must refuse")
	}
}

// The schema gate refuses every sweep write the way it refuses the others.
func TestSweepWritesHonourTheGate(t *testing.T) {
	p := NewGated("v0")
	for name, err := range map[string]error{
		"archive":   p.Archive([]string{"t-2tbn"}),
		"unarchive": p.Unarchive([]string{"t-2tbn"}),
		"tidy":      p.Tidy(board.TidyDoneDeps),
	} {
		if err == nil {
			t.Errorf("%s must refuse on a gated board", name)
		}
	}
}

// A quick add archived this session stays archived across Reload AND is
// listed in the archive store, so it can be restored like any other task.
func TestSweepArchivedQuickAddStaysArchivedAndRestorable(t *testing.T) {
	p := NewWith(sweepBoard())
	id, err := p.Add("added then retired", board.AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// Age and close it in place: the fixture's Board is the store.
	tk := p.Board().Task(id)
	tk.Status, tk.Closed = "done", time.Now().Add(-90*24*time.Hour)
	if err := p.Archive([]string{id}); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if p.Board().Task(id) != nil {
		t.Fatal("Reload resurrected the archived quick add")
	}
	s, _ := p.SweepPreview()
	found := false
	for _, a := range s.Archived {
		found = found || a.ID == id
	}
	if !found {
		t.Fatalf("the archived add is missing from the archive store: %+v", s.Archived)
	}
	if err := p.Unarchive([]string{id}); err != nil {
		t.Fatal(err)
	}
	if p.Board().Task(id) == nil {
		t.Error("unarchive did not bring the add back")
	}
}

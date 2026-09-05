package furrowstore

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/akira-toriyama/ridge/internal/board"
)

// labClose moves id to done and back-dates its closed stamp by days through
// the shard, the one way to age a task in a throwaway store: furrow stamps
// `closed` itself and has no flag to set it.
func labClose(t *testing.T, dir, id string, days int) {
	t.Helper()
	lab(t, dir, "furrow", "set", id, "-s", "done")
	path := dir + "/.furrow/tasks/" + id + ".json"
	raw := lab(t, dir, "cat", path)
	var shard map[string]any
	if err := json.Unmarshal(raw, &shard); err != nil {
		t.Fatal(err)
	}
	shard["closed"] = time.Now().UTC().Add(-time.Duration(days) * 24 * time.Hour).Format(time.RFC3339)
	out, _ := json.MarshalIndent(shard, "", "  ")
	lab(t, dir, "sh", "-c", "cat > "+path+" <<'EOF'\n"+string(out)+"\nEOF")
}

// The sweep contract against the real CLI: the archive dry-run names the
// aged done tasks and only them, tidy names the satisfied edges, the id-form
// archive moves exactly the list, the archive store lists them, unarchive
// brings one back, and tidy --done-deps prunes the class. Every shape the
// adapter decodes is pinned here.
//
// bite-exempt: execs a real furrow binary and always skips where furrow is not
func TestContractSweepRoundTrip(t *testing.T) {
	p, dir := newLabProvider(t)
	old := labAdd(t, dir, "aged done")
	fresh := labAdd(t, dir, "fresh done")
	open := labAdd(t, dir, "open, waits on the aged one", "--dep", old)
	labClose(t, dir, old, 60)
	lab(t, dir, "furrow", "set", fresh, "-s", "done")

	s, err := p.SweepPreview()
	if err != nil {
		t.Fatal(err)
	}
	if s.OlderThanDays <= 0 {
		t.Errorf("older_than_days = %d, want furrow's config default echoed", s.OlderThanDays)
	}
	if len(s.Archivable) != 1 || s.Archivable[0].ID != old || s.Archivable[0].Closed.IsZero() {
		t.Fatalf("archivable = %+v, want the aged task alone", s.Archivable)
	}
	if len(s.DoneDeps) != 1 || s.DoneDeps[0].ID != open || strings.Join(s.DoneDeps[0].Deps, ",") != old {
		t.Errorf("done_deps = %+v, want %s → %s", s.DoneDeps, open, old)
	}
	if len(s.UnknownKeys) != 0 || len(s.Archived) != 0 {
		t.Errorf("unknown/archived = %d/%d, want empty on a fresh store", len(s.UnknownKeys), len(s.Archived))
	}

	// The id-form refusals reach the adapter as furrow's envelope, not as a
	// move: an open id, a missing id, and the empty list (refused before exec).
	if err := p.Archive([]string{open}); err == nil || !strings.Contains(err.Error(), "done-lane") {
		t.Errorf("archive of an open task = %v", err)
	}
	if err := p.Archive([]string{"t-zzzzz"}); err == nil || !strings.Contains(err.Error(), "not-found") {
		t.Errorf("archive of a missing id = %v", err)
	}
	if err := p.Archive(nil); err == nil || !strings.Contains(err.Error(), "empty id list") {
		t.Errorf("archive of nothing = %v", err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if p.Board().Task(old) == nil {
		t.Fatal("a refused archive moved something")
	}

	// The write: exactly the list, the fresh done task stays.
	if err := p.Archive([]string{old}); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if p.Board().Task(old) != nil || p.Board().Task(fresh) == nil {
		t.Errorf("after archive: old on board=%v fresh on board=%v", p.Board().Task(old) != nil, p.Board().Task(fresh) != nil)
	}
	s, err = p.SweepPreview()
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Archived) != 1 || s.Archived[0].ID != old || s.Archived[0].Title != "aged done" {
		t.Errorf("archived = %+v, want the retired task", s.Archived)
	}
	// The satisfied edge now points at an ARCHIVED task; whatever tidy says
	// about it is furrow's call — pinned as observed so a change is visible.
	t.Logf("done_deps with the dep archived: %+v", s.DoneDeps)

	// Unarchive: a hot id is exit 2, a miss is exit 1 with nothing restored,
	// the real one comes back in the done lane with its stamp.
	if err := p.Unarchive([]string{fresh}); err == nil || !strings.Contains(err.Error(), "already on the hot board") {
		t.Errorf("unarchive of a hot task = %v", err)
	}
	if err := p.Unarchive([]string{old, "t-zzzzz"}); err == nil || !strings.Contains(err.Error(), "nothing was restored") {
		t.Errorf("unarchive with a miss = %v", err)
	}
	if err := p.Unarchive([]string{old}); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if tk := p.Board().Task(old); tk == nil || tk.Status != "done" || tk.Closed.IsZero() {
		t.Errorf("restored = %+v, want back in done with its closed stamp", tk)
	}

	// Tidy: the class goes, updated does not move.
	before := p.Board().Task(open).Updated
	if err := p.Tidy(board.TidyDoneDeps); err != nil {
		t.Fatal(err)
	}
	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if tk := p.Board().Task(open); len(tk.Deps) != 0 || !tk.Updated.Equal(before) {
		t.Errorf("after tidy: deps=%v updated moved=%v", tk.Deps, !tk.Updated.Equal(before))
	}
	if err := p.Tidy(board.TidyUnknownKeys); err != nil {
		t.Errorf("tidy --unknown-keys on a clean store = %v, want applied on an empty class", err)
	}
	s, _ = p.SweepPreview()
	if len(s.DoneDeps) != 0 {
		t.Errorf("done_deps after tidy = %+v", s.DoneDeps)
	}
}

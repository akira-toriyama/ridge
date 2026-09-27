package furrowstore

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The argv both commands compose, pinned without a binary: the flags, the
// `--` fence before every positional, and duplicates collapsed in order.
func TestRmArgvFencesTheIDsAndCollapsesDuplicates(t *testing.T) {
	args, unique := rmArgs([]string{"t-a", "t-a", "t-b"}, board.RemoveOptions{Force: true, Apply: true})
	if want := []string{"rm", "--json", "--force", "--yes", "--", "t-a", "t-b"}; !slices.Equal(args, want) {
		t.Errorf("rm argv = %q, want %q", args, want)
	}
	if want := []string{"t-a", "t-b"}; !slices.Equal(unique, want) {
		t.Errorf("unique = %q, want %q", unique, want)
	}
	if args, _ := rmArgs([]string{"t-a"}, board.RemoveOptions{}); !slices.Equal(args, []string{"rm", "--json", "--", "t-a"}) {
		t.Errorf("a plain dry run = %q", args)
	}
	if got := epicRmArgs("e-x", board.RemoveOptions{Force: true}); !slices.Equal(got, []string{"epic", "rm", "--json", "--force", "--", "e-x"}) {
		t.Errorf("epic rm argv = %q", got)
	}
}

// cannedFurrow is a `furrow` that prints one reply and exits 0, for the
// checks the adapter makes ON the reply — a real furrow cannot be made to
// answer a dry run with dry_run=false, or name fewer targets than asked.
func cannedFurrow(t *testing.T, stdout string) *Store {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "furrow")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\ncat <<'JSON'\n"+stdout+"\nJSON\n"), 0o755); err != nil { //nolint:gosec // an executable stub is the point
		t.Fatal(err)
	}
	return &Store{c: &furrowClient{bin: bin, dir: dir, timeout: 5 * time.Second}}
}

// The asset transfer's spelling (furrow #345, measured on dev 2026-09-27:
// `assets: {deleted: [names], kept: [{name, held_by: [ids]}]}`), pinned by
// a canned reply because CI's v6.0.0 cannot emit the key at all, and every
// other key of the report beside it.
func TestRmReplyDecodesTheWholeReportAssetsIncluded(t *testing.T) {
	p := cannedFurrow(t, `{"dry_run":true,"force":true,
"tasks":[{"id":"t-a","title":"取り下げる","status":"backlog","priority":10,"repeat":"FREQ=WEEKLY","due":"2027-03-31T14:59:59Z"}],
"references":{"deps":[{"from":"t-b","to":"t-a"}],"links":[{"body":"t-c","to":"t-a"}],"members":[],"epic_deps":[]},
"assets":{"copied":[],"deleted":["t-a-shot.png"],"kept":[{"name":"t-a-map.png","held_by":["t-d","e-x"]}]}}`)
	rep, err := p.Remove([]string{"t-a"}, board.RemoveOptions{Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if !rep.DryRun || !rep.Force || len(rep.Tasks) != 1 || rep.Tasks[0].Repeat != "FREQ=WEEKLY" || rep.Tasks[0].Due.IsZero() {
		t.Errorf("report = %+v", rep)
	}
	if !slices.Equal(rep.References.Deps, []board.RefEdge{{From: "t-b", To: "t-a"}}) || !slices.Equal(rep.References.Links, []board.RefLink{{Body: "t-c", To: "t-a"}}) {
		t.Errorf("references = %+v", rep.References)
	}
	if !slices.Equal(rep.Assets.Deleted, []string{"t-a-shot.png"}) || len(rep.Assets.Kept) != 1 ||
		rep.Assets.Kept[0].Name != "t-a-map.png" || !slices.Equal(rep.Assets.Kept[0].HeldBy, []string{"t-d", "e-x"}) {
		t.Errorf("assets = %+v", rep.Assets)
	}
	// And the pinned release's shape — no assets key at all — is the empty
	// transfer, not an error.
	p = cannedFurrow(t, `{"dry_run":true,"force":true,"tasks":[{"id":"t-a","title":"x"}],"references":{"deps":[],"links":[],"members":[],"epic_deps":[]}}`)
	rep, err = p.Remove([]string{"t-a"}, board.RemoveOptions{Force: true})
	if err != nil || len(rep.Assets.Deleted)+len(rep.Assets.Kept) != 0 {
		t.Errorf("v6.0.0's report (no assets key) = %+v, %v", rep, err)
	}
}

// The reply is held to the call: a dry run that says it applied, an apply
// that says it did not, a report naming fewer targets than asked, a box
// report naming another box — each refused rather than trusted.
func TestRmReplyIsHeldToTheCall(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply string
		opts  board.RemoveOptions
		want  string
	}{
		{"a dry run claiming to have applied", `{"dry_run":false,"force":false,"tasks":[{"id":"t-a"}],"references":{}}`, board.RemoveOptions{}, "refusing to trust"},
		{"an apply claiming a dry run", `{"dry_run":true,"force":false,"tasks":[{"id":"t-a"}],"references":{}}`, board.RemoveOptions{Apply: true}, "refusing to trust"},
		{"fewer targets than asked", `{"dry_run":true,"force":true,"tasks":[{"id":"t-a"}],"references":{}}`, board.RemoveOptions{Force: true}, "names 1 of 2 targets"},
	} {
		p := cannedFurrow(t, tc.reply)
		ids := []string{"t-a"}
		if strings.Contains(tc.want, "of 2") {
			ids = []string{"t-a", "t-b"}
		}
		_, err := p.Remove(ids, tc.opts)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%s: err = %v, want one saying %q", tc.name, err, tc.want)
		}
	}
	p := cannedFurrow(t, `{"dry_run":true,"force":true,"epic":{"id":"e-other","title":"x"},"references":{}}`)
	if _, err := p.EpicRemove("e-x", board.RemoveOptions{Force: true}); err == nil || !strings.Contains(err.Error(), "does not name e-x") {
		t.Errorf("a box report naming another box: err = %v", err)
	}
}

// The box's active flag rides the report (measured on dev 2026-09-27:
// `epic.active: true` on the withdrawal of the active box), and the gate's
// slot warning reads it from there.
func TestEpicRmReplyCarriesTheActiveFlag(t *testing.T) {
	p := cannedFurrow(t, `{"dry_run":true,"force":true,"epic":{"id":"e-x","title":"箱","active":true},"references":{"deps":[],"links":[],"members":[],"epic_deps":[]}}`)
	rep, err := p.EpicRemove("e-x", board.RemoveOptions{Force: true})
	if err != nil || rep.Epic == nil || !rep.Epic.Active || rep.Epic.Title != "箱" {
		t.Errorf("report = %+v, %v — want the active box as furrow reported it", rep, err)
	}
}

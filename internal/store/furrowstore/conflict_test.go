package furrowstore

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// furrow's sync-conflict message, verbatim (internal/app/sync.go, measured
// on dev 2026-09-28 by forcing a conflict between two clones): 181 cells.
const syncConflictMessage = "pull --rebase hit conflicts; the rebase was aborted and the board restored (your local sync commit is intact). Resolve the paths by hand (pull, fix, commit), then re-run furrow sync"

// A sync-conflict envelope names its paths in the error the ui shows — AHEAD
// of the message, since the status row is one 240-cell line truncated at the
// right and `synced: ` plus the message plus the kind already fills 200 of
// them; the adapter once dropped details.paths entirely, and then appended
// them where the floor cut them (t-36k0).
func TestFurrowErrorLeadsWithTheConflictedPaths(t *testing.T) {
	raw := `{"error":{"kind":"sync-conflict","retryable":false,"exit":3,"message":` + jsonString(syncConflictMessage) +
		`,"details":{"paths":[".furrow/tasks/t-3fq4e.json"]}}}`
	var env errorEnvelope
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	want := "conflicted paths: .furrow/tasks/t-3fq4e.json — " + syncConflictMessage + " (sync-conflict)"
	if got := env.Error.Error(); got != want {
		t.Errorf("Error() =\n %q\nwant\n %q", got, want)
	}

	// Five shard paths through the JSON: three named, the rest counted, and
	// the clause inside the status row's floor behind the ui's `⚠ synced: `
	// lead, however long furrow's prose after it.
	raw = `{"error":{"kind":"sync-conflict","message":` + jsonString(syncConflictMessage) +
		`,"details":{"paths":[".furrow/tasks/t-3fq4e.json",".furrow/epics/e-fmzj4.json",".furrow/bodies/t-3fq4e.md",".furrow/tasks/t-a2cwy.json",".furrow/meta.json"]}}}`
	env = errorEnvelope{}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	got := env.Error.Error()
	clause, _, ok := strings.Cut(got, " — ")
	if !ok || clause != "conflicted paths: .furrow/tasks/t-3fq4e.json, .furrow/epics/e-fmzj4.json, .furrow/bodies/t-3fq4e.md +2 more" {
		t.Errorf("a long list is capped and counted at the head: %q", got)
	}
	if w := ansi.StringWidth("⚠ synced: " + clause); w > 240 {
		t.Errorf("the clause must sit inside the 240-cell status row, is %d cells", w)
	}

	// sync-unmerged carries paths too, but its message names them already:
	// no clause. An envelope without details reads as before.
	raw = `{"error":{"kind":"sync-unmerged","message":"the working tree has unmerged paths (.furrow/tasks/t-3fq4e.json); resolve them, then re-run furrow sync","details":{"paths":[".furrow/tasks/t-3fq4e.json"]}}}`
	env = errorEnvelope{}
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatal(err)
	}
	if got := env.Error.Error(); strings.Contains(got, "conflicted paths") {
		t.Errorf("sync-unmerged must not repeat its paths: %q", got)
	}
	for _, raw := range []string{
		`{"error":{"kind":"referenced","subject":"t-x","message":"t-x is referenced","details":{"references":[{"from":"t-y","kind":"dep"}]}}}`,
		`{"error":{"kind":"session-busy","retryable":true,"message":"another session is busy","details":{"session":"abc"}}}`,
		`{"error":{"kind":"not-found","subject":"t-x","message":"unknown task"}}`,
	} {
		env = errorEnvelope{}
		if err := json.Unmarshal([]byte(raw), &env); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
		if got := env.Error.Error(); strings.Contains(got, "paths") {
			t.Errorf("a kind without paths must read as before: %q", got)
		}
	}
	plain := &furrowError{Kind: "not-found", Subject: "t-x", Message: "unknown task"}
	if got := plain.Error(); got != "unknown task (not-found: t-x)" {
		t.Errorf("no details, no clause: %q", got)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

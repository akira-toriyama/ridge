package views

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeViews(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "views.toml")
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func manyUnknownKeys(n int) string {
	var b strings.Builder
	b.WriteString("[[view]]\nname = \"a\"\nq = \"\"\n")
	for i := 0; i < n; i++ {
		fmt.Fprintf(&b, "unknown_%d = \"x\"\n", i)
	}
	return b.String()
}

// The strict pass is quadratic in the number of unknown keys, and every
// warning it returns is joined into a status line the UI re-measures on every
// frame. A hand-edit has a typo or two, so the report is bounded at both ends:
// a few thousand keys used to cost seconds of startup and then a warning
// string long enough to be re-measured forever.
func TestUnknownKeyWarningsAreBounded(t *testing.T) {
	// Under the line guard on purpose: this pins the REPORT cap, not the skip.
	const keys = 300
	_, warns, err := Load(writeViews(t, manyUnknownKeys(keys)))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(warns) > maxViewWarnings+1 {
		t.Errorf("%d unknown keys produced %d warnings, want at most %d",
			keys, len(warns), maxViewWarnings+1)
	}
	if last := warns[len(warns)-1]; !strings.Contains(last, "more views.toml warning") {
		t.Errorf("the bounded report does not say how many it dropped: %q", last)
	}
}

// Past the line limit the strict decode is skipped outright — it is the only
// way to bound a cost that lives inside the TOML decoder. The skip has to SAY
// so rather than look like a clean file. The guard counts LINES because the
// cost scales with the number of KEYS: short keys pack thousands into a small
// file, so a byte budget left seconds of work inside the "safe" region.
func TestAnOversizeFileSkipsTheUnknownKeyScanAndSaysSo(t *testing.T) {
	body := manyUnknownKeys(maxScanLines + 100)
	if len(body) > 32<<10 {
		t.Fatalf("the body is %d bytes — big enough that a byte budget would have caught it too, "+
			"so this test would not prove the guard counts lines", len(body))
	}
	_, warns, err := Load(writeViews(t, body))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "unknown keys not checked") {
		t.Errorf("an oversize file reported %v, want one line saying the check was skipped", warns)
	}
}

// clamp is the other warning source, and the UI joins BOTH into one status
// string it re-measures on every frame. Bounding only the unknown-key half
// left a 469-view file producing a 140KB status line.
func TestClampWarningsAreBoundedToo(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 60; i++ {
		fmt.Fprintf(&b, "[[view]]\nname = \"v%d\"\nq = \"\"\nlayout = \"nonsense\"\nsort = \"nonsense\"\nslice = \"nonsense\"\n", i)
	}
	_, warns, err := Load(writeViews(t, b.String()))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(warns) == 0 {
		t.Fatal("60 views with three bad fields each produced no clamp warnings; the fixture no longer bites")
	}
	if len(warns) > maxViewWarnings+1 {
		t.Errorf("clamp produced %d warnings, want the report bounded at %d", len(warns), maxViewWarnings+1)
	}
	joined := strings.Join(warns, "; ")
	if len(joined) > 4096 {
		t.Errorf("the joined status line is %d bytes; the UI re-measures it every frame", len(joined))
	}
}

// The bounds must not cost an ordinary file its report: one typo, one warning.
func TestAHandEditedTypoIsStillReported(t *testing.T) {
	_, warns, err := Load(writeViews(t, "[[view]]\nname = \"a\"\nq = \"\"\nlayotu = \"board\"\n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "layotu") {
		t.Errorf("got %v, want the one misspelled key named", warns)
	}
}

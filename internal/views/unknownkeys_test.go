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
	_, warns, err := Load(writeViews(t, manyUnknownKeys(500)))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(warns) > maxUnknownKeyWarnings+1 {
		t.Errorf("500 unknown keys produced %d warnings, want at most %d",
			len(warns), maxUnknownKeyWarnings+1)
	}
	if last := warns[len(warns)-1]; !strings.Contains(last, "more unknown keys") {
		t.Errorf("the bounded report does not say how many it dropped: %q", last)
	}
}

// Past the size limit the strict decode is skipped outright — it is the only
// way to bound a cost that lives inside the TOML decoder. The skip has to
// SAY so rather than look like a clean file.
func TestAnOversizeFileSkipsTheUnknownKeyScanAndSaysSo(t *testing.T) {
	body := manyUnknownKeys(2000)
	if len(body) <= strictScanLimit {
		t.Fatalf("the fixture body is %d bytes, not past the %d limit", len(body), strictScanLimit)
	}
	_, warns, err := Load(writeViews(t, body))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(warns) != 1 || !strings.Contains(warns[0], "unknown keys not checked") {
		t.Errorf("an oversize file reported %v, want one line saying the check was skipped", warns)
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

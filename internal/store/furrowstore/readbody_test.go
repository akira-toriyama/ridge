package furrowstore

import (
	"os"
	"path/filepath"
	"testing"
)

// ReadBody reads the file the load recorded for the id — the record as the
// store holds it now, not the snapshot — answers empty for an id whose JSON
// named no file, and furrow's not-found for an id the load never saw.
func TestReadBodyReadsTheRecordedFileOrAnswersEmptyOrUnknown(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "t-a.md")
	if err := os.WriteFile(path, []byte("# a\n\nfirst\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	p := &Store{bodyPaths: map[string]string{"t-a": path, "t-none": ""}}
	if got, err := p.ReadBody("t-a"); err != nil || got != "# a\n\nfirst\n" {
		t.Errorf("ReadBody(t-a) = %q, %v", got, err)
	}
	// The file moves under the snapshot: the read follows the file.
	if err := os.WriteFile(path, []byte("# a\n\nfirst\n\nsecond\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got, _ := p.ReadBody("t-a"); got != "# a\n\nfirst\n\nsecond\n" {
		t.Errorf("ReadBody must read the file now, got %q", got)
	}
	if got, err := p.ReadBody("t-none"); err != nil || got != "" {
		t.Errorf("an id with no file reads empty: %q, %v", got, err)
	}
	if _, err := p.ReadBody("t-ghost"); err == nil {
		t.Error("an id the load never saw is an error")
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if _, err := p.ReadBody("t-a"); err == nil {
		t.Error("a recorded file that is gone is an error, not an empty record")
	}
}

package views

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadMissingFileIsZeroViews(t *testing.T) {
	vs, warns, err := Load(filepath.Join(t.TempDir(), "views.toml"))
	if err != nil || vs != nil || warns != nil {
		t.Fatalf("missing file: got (%v, %v, %v), want (nil, nil, nil)", vs, warns, err)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "views.toml") // first save creates the dir
	in := []View{
		{Name: "今週の締切", Layout: "roadmap", Q: "is:actionable"},
		{Name: "needs review", Layout: "table", Sort: "due asc", Slice: `label:needs review`},
		{Name: "board only", Q: "repo:akira-toriyama/ridge"},
	}
	if err := Save(path, in); err != nil {
		t.Fatalf("Save: %v", err)
	}
	out, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("round trip produced warnings: %q", warns)
	}
	if len(out) != len(in) {
		t.Fatalf("round trip lost views: got %d, want %d", len(out), len(in))
	}
	for i := range in {
		if out[i] != in[i] {
			t.Errorf("view %d: got %+v, want %+v", i, out[i], in[i])
		}
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat: %v", err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Errorf("mode: got %o, want 644 (a view list is shareable data, not a keystroke log)", got)
	}
}

func TestSaveReplacesExistingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "views.toml")
	if err := Save(path, []View{{Name: "one"}, {Name: "two"}}); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	if err := Save(path, []View{{Name: "only"}}); err != nil {
		t.Fatalf("second Save: %v", err)
	}
	out, _, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(out) != 1 || out[0].Name != "only" {
		t.Fatalf("second save did not replace the file: %+v", out)
	}
}

func TestLoadMalformedTOMLIsTheOneHardError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "views.toml")
	if err := os.WriteFile(path, []byte("[[view]\nname = broken"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("malformed TOML loaded without error")
	}
}

// TestLoadClampsSemanticsWithOneWarningEach: a typo costs one field of one
// view, never the session — and every repair says so.
func TestLoadClampsSemanticsWithOneWarningEach(t *testing.T) {
	path := filepath.Join(t.TempDir(), "views.toml")
	src := `
[[view]]
name = ""
layout = "graph"

[[view]]
name = "bad sort"
sort = "priority"

[[view]]
name = "bad slice"
slice = "lane:done"

[[view]]
name = "unspeakable"
slice = 'label:say-"why"'

[[view]]
name = "clean"
layout = "table"
sort = "due"
slice = "epic:e-xxxx"
`
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	vs, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(vs) != 5 {
		t.Fatalf("got %d views, want 5 (clamped, not dropped)", len(vs))
	}
	// view 1 earns two (no name AND a bad layout); the other three earn one each.
	if len(warns) != 5 {
		t.Fatalf("got %d warnings %q, want 5", len(warns), warns)
	}
	if vs[0].Name != "view 1" || vs[0].Layout != "" {
		t.Errorf("view 1 not clamped: %+v", vs[0])
	}
	if vs[1].Sort != "" {
		t.Errorf("bad sort kept: %+v", vs[1])
	}
	if vs[2].Slice != "" || vs[3].Slice != "" {
		t.Errorf("bad slices kept: %+v / %+v", vs[2], vs[3])
	}
	if vs[4] != (View{Name: "clean", Layout: "table", Sort: "due", Slice: "epic:e-xxxx"}) {
		t.Errorf("clean view mangled: %+v", vs[4])
	}
	for i, w := range warns {
		if !strings.Contains(w, "view ") {
			t.Errorf("warning %d does not name its view: %q", i, w)
		}
	}
}

// TestLoadStripsControlCharacters: every one of these strings is rendered
// into a single chrome row, and a TOML basic string can spell \n or \u001b
// without a raw byte in the file — one such name sheared the whole frame
// under the title row (found by review).
func TestLoadStripsControlCharacters(t *testing.T) {
	path := filepath.Join(t.TempDir(), "views.toml")
	src := "[[view]]\nname = \"火の\\n粉\"\nq = \"label:\\u001b[31mred\"\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	vs, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(vs) != 1 {
		t.Fatalf("got %d views, want 1", len(vs))
	}
	if strings.ContainsAny(vs[0].Name+vs[0].Q, "\n\x1b") {
		t.Errorf("control characters survived: %+v", vs[0])
	}
	if vs[0].Name != "火の 粉" {
		t.Errorf("name: got %q, want the newline replaced by a space", vs[0].Name)
	}
	if len(warns) != 2 {
		t.Errorf("got %d warnings %q, want one per repaired field", len(warns), warns)
	}
}

// TestLoadWarnsOnUnknownKeys: a misspelled KEY is the likelier hand-edit
// than a misspelled value, and it used to be the one typo with no report.
// Still a warning, never an error — unknown keys stay ignored on purpose
// (forward-compat).
func TestLoadWarnsOnUnknownKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "views.toml")
	src := "[[view]]\nname = \"ok\"\nlayuot = \"table\"\n"
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	vs, warns, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(vs) != 1 || vs[0].Layout != "" {
		t.Fatalf("unknown key changed the decode: %+v", vs)
	}
	found := false
	for _, w := range warns {
		if strings.Contains(w, "layuot") {
			found = true
		}
	}
	if !found {
		t.Errorf("warnings %q do not name the misspelled key", warns)
	}
}

// TestSaveWritesThroughASymlink: a dotfiles-managed views.toml is a symlink,
// and a bare temp+rename replaced the link with a regular file, orphaning
// the target (found by review).
func TestSaveWritesThroughASymlink(t *testing.T) {
	target := filepath.Join(t.TempDir(), "dotfiles", "views.toml")
	if err := Save(target, []View{{Name: "old"}}); err != nil {
		t.Fatalf("seed Save: %v", err)
	}
	linkDir := t.TempDir()
	link := filepath.Join(linkDir, "views.toml")
	if err := os.Symlink(target, link); err != nil {
		t.Fatalf("Symlink: %v", err)
	}
	if err := Save(link, []View{{Name: "new"}}); err != nil {
		t.Fatalf("Save through link: %v", err)
	}
	if fi, err := os.Lstat(link); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Errorf("the link was replaced by a regular file (mode %v, err %v)", fi.Mode(), err)
	}
	got, _, err := Load(target)
	if err != nil || len(got) != 1 || got[0].Name != "new" {
		t.Errorf("the TARGET did not receive the write: (%+v, %v)", got, err)
	}
}

func TestSplitSort(t *testing.T) {
	cases := []struct {
		in       string
		key, dir string
		ok       bool
	}{
		{"due", "due", "", true},
		{"due asc", "due", "asc", true},
		{"updated desc", "updated", "desc", true},
		{"  value   asc ", "value", "asc", true}, // Fields tolerates spacing
		{"priority", "", "", false},
		{"due sideways", "", "", false},
		{"due asc extra", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		key, dir, ok := SplitSort(c.in)
		if key != c.key || dir != c.dir || ok != c.ok {
			t.Errorf("SplitSort(%q) = (%q, %q, %v), want (%q, %q, %v)",
				c.in, key, dir, ok, c.key, c.dir, c.ok)
		}
	}
}

func TestDefaultPathHonoursOnlyAbsoluteXDG(t *testing.T) {
	abs := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", abs)
	p, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	if want := filepath.Join(abs, "ridge", "views.toml"); p != want {
		t.Errorf("absolute XDG: got %q, want %q", p, want)
	}

	t.Setenv("XDG_CONFIG_HOME", "relative/dir") // the XDG spec says ignore it
	p, err = DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	home, _ := os.UserHomeDir()
	if want := filepath.Join(home, ".config", "ridge", "views.toml"); p != want {
		t.Errorf("relative XDG: got %q, want %q", p, want)
	}
}

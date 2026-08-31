// Package views is the saved-view store: named {layout, q, sort, slice}
// bundles, persisted in one TOML file the user owns (views.toml under
// ~/.config/ridge). It knows nothing of the TUI or the board — pure data in,
// data out; the caller resolves the path (DefaultPath) and decides when to
// read or write.
//
// views.toml is the ONE file ridge writes, and only on the explicit save
// gesture. It is user DATA — the same kind of thing the board is — not
// configuration, which is why the house config rule (read-only source of
// truth) does not apply to it. Nothing writes it automatically: a session
// that never saves never touches it, and a missing file is simply zero
// views. Save rewrites the whole file canonically, so hand-written comments
// do not survive a save — the file's authority is its VALUES.
package views

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	toml "github.com/pelletier/go-toml/v2"
)

// View is one saved view: a name on a bundle of display state. Every field
// is the canonical string spelling — the TUI's enums map to and from these
// at the boundary, and a test over the vocabulary slices below pins the two
// against drift.
type View struct {
	Name string `toml:"name"`
	// Layout is board | table | roadmap ("" reads as board). The other
	// full-screen views are absent on purpose: a graph is rooted on a task
	// and the map/box overviews are population toggles — none of them is a
	// state a NAMED view could reproduce from cold.
	Layout string `toml:"layout,omitempty"`
	// Q is the typed half of the filter, verbatim furrow -q passthrough.
	Q string `toml:"q,omitempty"`
	// Sort is "<key>[ asc| desc]" over the table's axes; direction defaults
	// to the key's natural one. Stored even when the layout is not table —
	// GH keeps a view's sort regardless, and the table is one `v` away.
	Sort string `toml:"sort,omitempty"`
	// Slice is "<field>:<value>", the panel's selection. The value is RAW
	// (no -q quoting): quoting is issue-time lexing, not identity.
	Slice string `toml:"slice,omitempty"`
}

// The vocabularies. These are the on-disk contract; the ui package's enums
// mirror them and a test walks both directions.
var (
	Layouts     = []string{"board", "table", "roadmap"}
	SortKeys    = []string{"updated", "created", "value", "effort", "due"}
	SliceFields = []string{"repo", "label", "epic"}
)

// file is the on-disk shape: a list of [[view]] tables.
type file struct {
	View []View `toml:"view"`
}

// Load reads and clamps a views file. A missing file is zero views and no
// error; a malformed TOML is the one hard error. Everything semantic is
// CLAMPED, never fatal — an unknown layout falls back to board, a bad sort
// or slice is dropped — with one warning per clamp, so a typo costs one
// field of one view, not the session.
func Load(path string) ([]View, []string, error) {
	b, err := os.ReadFile(path) //nolint:gosec // the caller chose the path; default is the user's own config dir
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil, nil
	}
	if err != nil {
		return nil, nil, err
	}
	var f file
	if err := toml.Unmarshal(b, &f); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	var warns []string
	for i := range f.View {
		f.View[i] = clamp(f.View[i], i, &warns)
	}
	return f.View, warns, nil
}

// clamp normalises one view in place of rejecting it, appending one warning
// per repaired field.
func clamp(v View, i int, warns *[]string) View {
	warn := func(format string, a ...any) {
		*warns = append(*warns, fmt.Sprintf("view %d (%s): %s", i+1, v.Name, fmt.Sprintf(format, a...)))
	}
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" {
		v.Name = fmt.Sprintf("view %d", i+1)
		warn("no name — called %q", v.Name)
	}
	v.Q = strings.TrimSpace(v.Q)
	if v.Layout != "" && !slices.Contains(Layouts, v.Layout) {
		warn("unknown layout %q (want %s) — board", v.Layout, strings.Join(Layouts, "|"))
		v.Layout = ""
	}
	if v.Sort != "" {
		if _, _, ok := SplitSort(v.Sort); !ok {
			warn("unknown sort %q (want \"<%s> [asc|desc]\") — dropped",
				v.Sort, strings.Join(SortKeys, "|"))
			v.Sort = ""
		}
	}
	if v.Slice != "" {
		f, val, found := strings.Cut(v.Slice, ":")
		switch {
		case !found || !slices.Contains(SliceFields, f):
			warn("unknown slice %q (want \"<%s>:<value>\") — dropped",
				v.Slice, strings.Join(SliceFields, "|"))
			v.Slice = ""
		case val == "":
			warn("slice %q has no value — dropped", v.Slice)
			v.Slice = ""
		case strings.Contains(val, `"`):
			// A double quote has no -q spelling (furrow's quoting has no
			// escape), so this slice could never be issued.
			warn("slice value %q cannot be spelled in -q — dropped", val)
			v.Slice = ""
		}
	}
	return v
}

// SplitSort parses the "<key>[ asc| desc]" spelling: the key, whether a
// direction was given as "asc", and whether the whole string is well-formed.
// When no direction is given, dir is returned as "" — the caller owes the
// key its natural direction.
func SplitSort(s string) (key, dir string, ok bool) {
	f := strings.Fields(s)
	if len(f) == 0 || len(f) > 2 || !slices.Contains(SortKeys, f[0]) {
		return "", "", false
	}
	if len(f) == 2 {
		if f[1] != "asc" && f[1] != "desc" {
			return "", "", false
		}
		dir = f[1]
	}
	return f[0], dir, true
}

// Save rewrites the whole file atomically (temp + rename in the same
// directory), creating the directory on first save. The atomic dance is not
// ceremony: the file is hand-edited too, and a crash mid-write must leave
// the old file, never half of the new one.
func Save(path string, vs []View) error {
	b, err := toml.Marshal(file{View: vs})
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".views-*.toml")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name()) //nolint:errcheck // gone already when the rename landed
	if _, err := tmp.Write(b); err != nil {
		tmp.Close() //nolint:errcheck,gosec // the write error is the story
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close() //nolint:errcheck,gosec // the sync error is the story
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	// CreateTemp opens 0600; this file is a shareable view list, not a
	// keystroke log (-debuglog's 0600 is for the latter), so it gets the
	// ordinary config mode a hand-written one would have.
	if err := os.Chmod(tmp.Name(), 0o644); err != nil { //nolint:gosec // G302: deliberately 0644, see above
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// DefaultPath is ~/.config/ridge/views.toml, honouring XDG_CONFIG_HOME only
// when it is absolute (the XDG rule). os.UserConfigDir is deliberately not
// used: on darwin it answers ~/Library/Application Support, and this file's
// documented home is ~/.config.
func DefaultPath() (string, error) {
	base := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(base) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, "ridge", "views.toml"), nil
}

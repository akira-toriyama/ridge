package ui

import (
	"fmt"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The SWEEP's row model — pure, deterministic, no lipgloss. sweepview.go
// paints it. One flat list of rows over three sections (archive candidates,
// tidy's two classes, the archive store), because the cursor walks them as
// one column: a section header is a row the cursor skips, an item is a row it
// can stop on and act from.
//
// Identity is a KEY (section + id), never an index: a preview re-read after a
// write reorders and shrinks the list, and the cursor must stay on the row it
// was on when that row survives.

type sweepSection int

const (
	sweepArchive     sweepSection = iota // furrow archive's aged done tasks
	sweepDoneDeps                        // furrow tidy --done-deps
	sweepUnknownKeys                     // furrow tidy --unknown-keys
	sweepArchived                        // furrow ls --archived, for unarchive
)

func (s sweepSection) String() string {
	switch s {
	case sweepArchive:
		return "archive"
	case sweepDoneDeps:
		return "tidy done-deps"
	case sweepUnknownKeys:
		return "tidy unknown-keys"
	case sweepArchived:
		return "archived"
	}
	return "unknown"
}

// sweepRow is one placed line.
type sweepRow struct {
	Key     string       // "" for a header / filler row (not selectable)
	Section sweepSection // the section the row belongs to
	ID      string       // task id; "" on headers and meta.json rows
	Header  bool
	Empty   bool // the "— nothing —" line under an empty section
}

func sweepKey(s sweepSection, id string) string { return fmt.Sprintf("%d\x00%s", int(s), id) }

// sweepRows lays the preview out as rows: for each section a header, then its
// items, or one Empty line when it has none. A nil preview (not read yet, or
// refused) yields the four headers with Empty lines, so the frame has a shape
// before the first verdict lands.
func sweepRows(s *board.Sweep) []sweepRow {
	var rows []sweepRow
	section := func(sec sweepSection, ids []string) {
		rows = append(rows, sweepRow{Section: sec, Header: true})
		if len(ids) == 0 {
			rows = append(rows, sweepRow{Section: sec, Empty: true})
			return
		}
		for _, id := range ids {
			rows = append(rows, sweepRow{Key: sweepKey(sec, id), Section: sec, ID: id})
		}
	}
	var a, d, u, r []string
	if s != nil {
		for _, t := range s.Archivable {
			a = append(a, t.ID)
		}
		for _, t := range s.DoneDeps {
			d = append(d, t.ID)
		}
		for i, t := range s.UnknownKeys {
			id := t.ID
			if id == "" {
				// meta.json and the like carry no id; the file is the identity.
				id = fmt.Sprintf("%s#%d", t.File, i)
			}
			u = append(u, id)
		}
		for _, t := range s.Archived {
			r = append(r, t.ID)
		}
	}
	section(sweepArchive, a)
	section(sweepDoneDeps, d)
	section(sweepUnknownKeys, u)
	section(sweepArchived, r)
	return rows
}

// sweepIndex resolves a key to its row index, -1 when the list no longer has it.
func sweepIndex(rows []sweepRow, key string) int {
	if key == "" {
		return -1
	}
	for i, r := range rows {
		if r.Key == key {
			return i
		}
	}
	return -1
}

// sweepFirst is the first selectable row's key, "" when every section is empty.
func sweepFirst(rows []sweepRow) string {
	for _, r := range rows {
		if r.Key != "" {
			return r.Key
		}
	}
	return ""
}

// sweepStep moves from key by dir over the selectable rows, skipping headers
// and Empty lines; it returns key unchanged at either end.
func sweepStep(rows []sweepRow, key string, dir int) string {
	i := sweepIndex(rows, key)
	if i < 0 {
		return sweepFirst(rows)
	}
	for j := i + dir; j >= 0 && j < len(rows); j += dir {
		if rows[j].Key != "" {
			return rows[j].Key
		}
	}
	return key
}

// sweepLast is the last selectable row's key.
func sweepLast(rows []sweepRow) string {
	for i := len(rows) - 1; i >= 0; i-- {
		if rows[i].Key != "" {
			return rows[i].Key
		}
	}
	return ""
}

// sweepArchiveSet is the archive candidates minus the ones the user skipped —
// the explicit id list the write sends, so what moves is exactly the rows
// the frame showed as included.
func sweepArchiveSet(s *board.Sweep, skipped map[string]bool) []string {
	if s == nil {
		return nil
	}
	var ids []string
	for _, t := range s.Archivable {
		if !skipped[t.ID] {
			ids = append(ids, t.ID)
		}
	}
	return ids
}

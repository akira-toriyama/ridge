package ui

import (
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// -filter and views.toml's q are free text, and lipgloss honours a line
// break: a query carrying one rendered as a BLOCK, so its tail landed in the
// lane-header row and took the ⚠ refusal with it — a query ridge had already
// refused looked accepted. The filter row is one line; a query folds onto it.
func TestAQueryWithALineBreakRendersAsTheSameOneLineQuery(t *testing.T) {
	const w, h = 240, 40
	for _, tc := range []struct{ name, broken, flat string }{
		{"refused", "is:AAA\nis:BBB", "is:AAA is:BBB"},
		{"accepted", "lane:backlog\nis:open", "lane:backlog is:open"},
		{"carriage return", "lane:backlog\ris:open", "lane:backlog is:open"},
		{"crlf", "lane:backlog\r\nis:open", "lane:backlog is:open"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			broken := dumpWithFilter(t, tc.broken, w, h)
			flat := dumpWithFilter(t, tc.flat, w, h)
			if broken != flat {
				t.Errorf("a query spelled with a break does not render as the flat one.\nbreak:\n%s\nflat:\n%s",
					firstLines(broken, 3), firstLines(flat, 3))
			}
		})
	}
}

func dumpWithFilter(t *testing.T, q string, w, h int) string {
	t.Helper()
	m := New(memstore.New(), Options{Filter: q})
	out, err := m.Dump(w, h, "", true)
	if err != nil {
		t.Fatalf("dump with filter %q: %v", q, err)
	}
	return out
}

func firstLines(s string, n int) string {
	lines := strings.Split(s, "\n")
	if len(lines) > n {
		lines = lines[:n]
	}
	return strings.Join(lines, "\n")
}

// The query itself reaches the store UNTOUCHED — the filter bar and
// `furrow ls -q` must never disagree about what a query means, and a quoted
// term can legitimately contain anything. Only the RENDER folds.
func TestALineBreakSurvivesIntoTheQueryThatReachesTheStore(t *testing.T) {
	m := New(memstore.New(), Options{Filter: "lane:backlog\nis:open"})
	if _, err := m.Dump(240, 40, "", true); err != nil {
		t.Fatalf("dump: %v", err)
	}
	if !strings.Contains(m.qRaw, "\n") {
		t.Errorf("qRaw = %q — the store's copy of the query was rewritten, not just its rendering", m.qRaw)
	}
}

// A furrow refusal is free text on the same one-line row, and a multi-line one
// used to paint over the board below it.
func TestAMultiLineRefusalStaysOnItsOwnRow(t *testing.T) {
	const w, h = 240, 40
	m := New(memstore.New(), Options{})
	if _, err := m.Dump(w, h, "", true); err != nil {
		t.Fatalf("dump: %v", err)
	}
	base := strings.Split(ansiStrip(m.View().Content), "\n")
	m.qErr = "furrow ls: exit 2\nunknown qualifier \"nope\"\n  did you mean \"note\"?"
	got := strings.Split(ansiStrip(m.View().Content), "\n")
	if len(got) != len(base) {
		t.Fatalf("a three-line refusal made the frame %d rows, want %d", len(got), len(base))
	}
	for i := range got {
		if i == 1 {
			continue // the filter row itself is expected to change
		}
		if got[i] != base[i] {
			t.Errorf("row %d changed when only the filter row should have:\n got: %s\nwant: %s",
				i, got[i], base[i])
		}
	}
}

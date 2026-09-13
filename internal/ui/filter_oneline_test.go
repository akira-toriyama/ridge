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

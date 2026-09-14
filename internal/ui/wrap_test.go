package ui

import (
	"strings"
	"testing"

	lg "charm.land/lipgloss/v2"
)

// The board is mostly Japanese, and Japanese has no spaces. A wrap that broke
// only on whitespace treated the 66-cell run after `行程表 v2 — ` as one
// word, carried it whole to the next line, and left the first line with 11 of
// its 73 cells — so a one-line graph node showed 11 cells of a 77-cell title.
func TestWrapLinesFillsTheLineBeforeBreakingAJapaneseRun(t *testing.T) {
	const title = "行程表 v2 — 阿蘇→高千穂を買い出しと温泉の寄り道込みで引き直す（雨天代替つき）"
	const w = 73
	got := wrapLines(title, w)
	if len(got) != 2 {
		t.Fatalf("wrapLines(%d) = %d lines %q, want 2", w, len(got), got)
	}
	// A double-width glyph may leave one cell short; anything less is a line
	// that gave up early.
	if first := lg.Width(got[0]); first < w-1 {
		t.Errorf("the first line uses %d of %d cells: %q", first, w, got[0])
	}
}

// Whitespace stays the preferred break for text that has it: an ASCII word is
// never split while a break before it was available.
func TestWrapLinesKeepsEnglishWordsWhole(t *testing.T) {
	const title = "a plain english title long enough to wrap across several lines at any of these widths"
	words := map[string]bool{}
	for _, f := range strings.Fields(title) {
		words[f] = true
	}
	for w := 8; w <= 80; w++ {
		for i, l := range wrapLines(title, w) {
			for _, f := range strings.Fields(l) {
				if !words[f] {
					t.Fatalf("w=%d line %d split a word: %q", w, i, l)
				}
			}
		}
	}
}

// Every line fits, nothing is lost, and the order holds — at every width,
// over Japanese, English and mixed titles.
func TestWrapLinesPreservesTextWithinWidth(t *testing.T) {
	titles := []string{
		"行程表 v2 — 阿蘇→高千穂を買い出しと温泉の寄り道込みで引き直す（雨天代替つき）",
		"予約の総ざらい — 温泉・レンタル品・雨天予備日をまとめて確定し、宿とレンタカーの取り消し期限を一覧にする",
		"a plain english title long enough to wrap across several lines at any of these widths",
		"mixed 日本語 and english 混在 title that wraps unevenly depending on the width given",
		"ridge: docs drift 22 件を直す（挙動系 5 件が先: edit menu の refs …）",
	}
	squash := func(s string) string { return strings.Join(strings.Fields(s), "") }
	for _, title := range titles {
		for w := 4; w <= 80; w++ {
			got := wrapLines(title, w)
			for i, l := range got {
				if lg.Width(l) > w {
					t.Fatalf("w=%d line %d is %d cells: %q", w, i, lg.Width(l), l)
				}
				if l == "" {
					t.Fatalf("w=%d line %d is empty: %q", w, i, got)
				}
			}
			if squash(strings.Join(got, "")) != squash(title) {
				t.Fatalf("w=%d lost or reordered text:\n%q\n%q", w, got, title)
			}
		}
	}
}

// A run with no break opportunity is hard-wrapped, an existing line break is
// kept, and the empty string is one empty line — what lipgloss did.
func TestWrapLinesEdges(t *testing.T) {
	for _, tc := range []struct {
		in   string
		w    int
		want []string
	}{
		{"abcdefghij", 4, []string{"abcd", "efgh", "ij"}},
		{"ab\ncd ef", 3, []string{"ab", "cd", "ef"}},
		{"", 10, []string{""}},
		{"a  b", 10, []string{"a  b"}},
		{"ab   cd", 3, []string{"ab", "cd"}},
	} {
		got := wrapLines(tc.in, tc.w)
		if strings.Join(got, "|") != strings.Join(tc.want, "|") {
			t.Errorf("wrapLines(%q, %d) = %q, want %q", tc.in, tc.w, got, tc.want)
		}
	}
}

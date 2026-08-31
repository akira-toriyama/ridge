package ui

import "testing"

// The label→colour map is frozen here because nothing else freezes it. Both
// plausible rewrites of chipIndex — ranging runes, or applying this repo's
// "measure with lipgloss.Width, never len()" rule to the hash — leave go
// build, go vet, golangci-lint, go test and every -plain dump byte-identical
// while recolouring most CJK labels on the board (measured 2026-08-28). A
// -plain dump cannot see it either: colour is exactly what -plain strips.
//
// slots is pinned rather than read from a theme's palette: what is frozen is
// the hash, not any label's current colour, so adding a hue is free.
func TestChipIndexHashesBytesNotRunesOrCells(t *testing.T) {
	const slots = 8
	// ASCII cannot discriminate: byte-wise, rune-wise and Width-limited FNV
	// agree on every ASCII name, which is why the whole fixture (gear, onsen,
	// bbq, bento) was blind to this. The CJK rows carry the test; the ASCII
	// rows only pin the shared baseline.
	for _, tc := range []struct {
		name string
		want int
	}{
		{"検証", 7},   // runes: 5, Width-limited: 3
		{"日本語", 7},  // runes: 6, Width-limited: 1
		{"バグ", 4},   // runes: 5, Width-limited: 6
		{"レビュー", 1}, // runes: 1 — agrees; Width-limited: 6
		{"依存", 4},   // runes: 0, Width-limited: 1
		{"テスト", 5},  // runes: 0, Width-limited: 1
		{"ui", 3},
		{"bbq", 4},
		{"", 5}, // the empty name is the bare FNV-1a offset basis, mod 8
	} {
		if got := chipIndex(tc.name, slots); got != tc.want {
			t.Errorf("chipIndex(%q, %d) = %d, want %d", tc.name, slots, got, tc.want)
		}
	}
}

// chipFor must route through chipIndex: hashing inlined back into chipFor
// would leave the frozen table above green while repainting every card.
//
// Both themes, because the hues are appended as {dark, light} PAIRS: a new
// pair whose light half repeats an existing light hue survives a dark-only
// check and ships two labels that are the same colour on every -light board.
func TestChipForPicksTheSlotChipIndexNames(t *testing.T) {
	for _, dark := range []bool{true, false} {
		th := newTheme(dark)

		// Without distinct slots the comparison below passes for any chipFor
		// at all, so the palette's distinctness is asserted, not assumed.
		at := map[string]int{}
		for i, hue := range th.chipHues {
			dot := hue.Render(glyphLaneDot)
			if j, dup := at[dot]; dup {
				t.Errorf("dark=%v: palette slots %d and %d render identically (%q): a label colour cannot name a slot", dark, j, i, dot)
			}
			at[dot] = i
		}

		for _, name := range []string{"検証", "日本語", "バグ", "レビュー", "依存", "テスト", "ui", "bbq", ""} {
			want := th.chipHues[chipIndex(name, len(th.chipHues))].Render(glyphLaneDot)
			if got := th.chipFor(name).Render(glyphLaneDot); got != want {
				t.Errorf("dark=%v: chipFor(%q) rendered %q, want slot %d's %q",
					dark, name, got, chipIndex(name, len(th.chipHues)), want)
			}
		}
	}
}

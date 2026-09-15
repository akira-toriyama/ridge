package board

import "testing"

// The -q facts QTerm encodes were measured against the real furrow binary
// (slicemode.go once held them): ASCII whitespace and a comma reinterpret a
// bare value, quoting suppresses both, and the CJK/NBSP spaces are not
// separators at all — so a value holding only those must NOT be quoted, or
// the quotes become part of the search.
func TestQTermQuotesOnlyWhatTheLexerWouldSplit(t *testing.T) {
	for _, tc := range []struct{ value, want string }{
		{"bbq", "label:bbq"},
		{"needs review", `label:"needs review"`},
		{"tui,cli", `label:"tui,cli"`},
		{"a\tb", "label:\"a\tb\""},
		{"全角　空白", "label:全角　空白"},
		{"nb sp", "label:nb sp"},
		{"", "label:"},
	} {
		if got := QTerm("label", tc.value); got != tc.want {
			t.Errorf("QTerm(label, %q) = %q, want %q", tc.value, got, tc.want)
		}
	}
}

func TestQSpellableRefusesADoubleQuote(t *testing.T) {
	if QSpellable(`say "hi"`) {
		t.Error("a value holding a double quote has no -q spelling")
	}
	if !QSpellable("needs review") || !QSpellable("") {
		t.Error("whitespace and emptiness are spellable (QTerm quotes the former)")
	}
}

// Whitespace between terms is the implicit AND; the typed query keeps its own
// text and the slice term rides after it. Empty parts vanish rather than
// leaving a stray space, and the typed text is trimmed so a trailing space
// never lands inside the composed query.
func TestQAndJoinsWithTheImplicitAnd(t *testing.T) {
	for _, tc := range []struct {
		parts []string
		want  string
	}{
		{[]string{"lane:backlog", "label:bbq"}, "lane:backlog label:bbq"},
		{[]string{"", "label:bbq"}, "label:bbq"},
		{[]string{"lane:backlog ", ""}, "lane:backlog"},
		{[]string{"  ", ""}, ""},
		{nil, ""},
	} {
		if got := QAnd(tc.parts...); got != tc.want {
			t.Errorf("QAnd(%q) = %q, want %q", tc.parts, got, tc.want)
		}
	}
}

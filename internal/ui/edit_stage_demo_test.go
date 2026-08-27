package ui

import (
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// The two sub-editor stages exist only between two keystrokes of a live
// overlay, so until t-36yr no -dump frame could show them: a regression that
// blanked either would have shipped unseen. These pin what each demo frame
// must prove; footer_test's DemoNames sweep already guarantees both render
// and differ from the bare board.
func TestEditPickDemoProvesThePicker(t *testing.T) {
	m := New(memstore.New(), Options{})
	frame, err := m.Dump(240, 60, "editpick", true)
	if err != nil {
		t.Fatal(err)
	}
	// The field header and the full key contract — blanking the pick body
	// drops both (measured against the blanked mutant).
	for _, want := range []string{"edit t-9sa6", "set value", "press 1-5 · 0 clears · esc back"} {
		if !strings.Contains(frame, want) {
			t.Errorf("-demo editpick: %q is missing from the frame", want)
		}
	}
}

// The input assertions are LINE-scoped on purpose: the seeded title's tail
// also appears in the peek header and on the selected card, so a whole-frame
// Contains stayed green with the seed deleted, the input un-rendered, and
// the cursor moved to the head — the same class footer_test's sweep note
// records. The one line carrying the prompt is the input; it must hold the
// value's TAIL and not its head, because the cursor sits at the end of a
// title wider than the window.
func TestEditInputDemoSeedsTheFocusedInput(t *testing.T) {
	m := New(memstore.New(), Options{})
	frame, err := m.Dump(240, 60, "editinput", true)
	if err != nil {
		t.Fatal(err)
	}
	var promptLines []string
	for _, l := range strings.Split(frame, "\n") {
		if strings.Contains(l, "> ") {
			promptLines = append(promptLines, l)
		}
	}
	if len(promptLines) != 1 {
		t.Fatalf("want exactly one prompt line in the frame, got %d: %q", len(promptLines), promptLines)
	}
	line := promptLines[0]
	if !strings.Contains(line, "積載図に落とす") {
		t.Errorf("the input line lost the seeded tail (seeded from t-9sa6's fixture title): %q", line)
	}
	if strings.Contains(line, "持ち物リスト") {
		t.Errorf("the input shows the value's head — the cursor must sit at the end: %q", line)
	}
	for _, want := range []string{"edit t-9sa6", "retitle", "⏎ apply · esc back"} {
		if !strings.Contains(frame, want) {
			t.Errorf("-demo editinput: %q is missing from the frame", want)
		}
	}
	// The frame cannot show focus (the compositor drops the cursor's SGR), so
	// the name's "Focused" is pinned on the model itself — a Blur() slipped in
	// after the seed would pass every frame assert.
	if m.edit == nil || !m.edit.input.Focused() {
		t.Error("the demo's input is not focused")
	}
}

// The deps sub-editor demo must prove both state glyphs at once — an open dep
// and a done one — plus the one-way key contract (remove, never toggle).
func TestEditDepsDemoProvesBothStates(t *testing.T) {
	m := New(memstore.New(), Options{})
	frame, err := m.Dump(240, 60, "editdeps", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"edit t-jv3j",
		"deps — waits on",
		"o t-ehk7",
		"v t-t38k",
		"⏎/x remove · a add · esc back",
	} {
		if !strings.Contains(frame, want) {
			t.Errorf("-demo editdeps: %q is missing from the frame", want)
		}
	}
}

// The refs list stage, same contract as the two pins above: it exists only
// mid-overlay, so this is the frame that would have shipped blank unseen.
func TestEditRefsDemoProvesTheList(t *testing.T) {
	m := New(memstore.New(), Options{})
	frame, err := m.Dump(240, 60, "editrefs", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"edit t-9sa6",
		"▌ docs/積載図-2026.md:18", // the cursor on the file:line row
		"https://camp.example.com/loading-guide",
		"⏎/x remove · a add · esc back",
	} {
		if !strings.Contains(frame, want) {
			t.Errorf("-demo editrefs: %q is missing from the frame", want)
		}
	}
}

// The note input demo, line-scoped like TestEditInputDemoSeedsTheFocusedInput:
// the one prompt line must hold the seeded value's TAIL (the cursor sits at
// the end of a value wider than the 48-cell window).
func TestNoteDemoSeedsTheFocusedInput(t *testing.T) {
	m := New(memstore.New(), Options{})
	frame, err := m.Dump(240, 60, "note", true)
	if err != nil {
		t.Fatal(err)
	}
	var promptLines []string
	for _, l := range strings.Split(frame, "\n") {
		if strings.Contains(l, "> ") {
			promptLines = append(promptLines, l)
		}
	}
	if len(promptLines) != 1 {
		t.Fatalf("want exactly one prompt line in the frame, got %d: %q", len(promptLines), promptLines)
	}
	if !strings.Contains(promptLines[0], "2案目から。") {
		t.Errorf("the input line lost the seeded tail: %q", promptLines[0])
	}
	for _, want := range []string{"edit t-9sa6", "append note — one paragraph onto the body", "⏎ apply · esc back"} {
		if !strings.Contains(frame, want) {
			t.Errorf("-demo note: %q is missing from the frame", want)
		}
	}
}

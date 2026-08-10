package memstore

import "testing"

// NewGated must stay gated across a reload. The obvious spelling — wrap a
// board once with NewWith — would have worked at startup and silently dropped
// the gate the first time `r` re-read the store, which is exactly the shape of
// bug this whole affordance exists to catch.
func TestGatedFixtureStaysReadOnlyAcrossReload(t *testing.T) {
	p := NewGated("board-behind")

	if p.Board().Writable() {
		t.Fatal("a gated fixture must not be writable")
	}
	if got := p.Board().SchemaState(); got != "board-behind" {
		t.Errorf("schema state = %q, want board-behind", got)
	}
	if n := len(p.Board().Tasks()); n == 0 {
		t.Error("a gated fixture still has to serve the fixture's tasks")
	}

	if err := p.Reload(); err != nil {
		t.Fatal(err)
	}
	if p.Board().Writable() {
		t.Error("reload dropped the gate — the read-only board became writable")
	}
	if got := p.Board().SchemaState(); got != "board-behind" {
		t.Errorf("after reload schema state = %q, want board-behind", got)
	}
}

// The ungated fixture is unaffected.
func TestPlainFixtureIsWritable(t *testing.T) {
	if !New().Board().Writable() {
		t.Error("the plain fixture must stay writable")
	}
}

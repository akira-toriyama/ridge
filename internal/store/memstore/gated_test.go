package memstore

import (
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
)

// NewGated must stay gated across a reload. The spelling that drops the gate
// is gating the board New() already serves while leaving New()'s rebuild
// closure in place: writable=false at startup, then writable=true and an
// empty schema after one Reload. (NewWith would NOT have that bug — it
// re-serves the same board — but it would silently replace New()'s documented
// contract of rebuilding a pristine board. Corrected after the independent
// review of PR #22 caught the original comment blaming the wrong spelling.)
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
	p := New()
	if !p.Board().Writable() {
		t.Error("the plain fixture must stay writable")
	}
	if err := p.PersistDone(p.Board().Tasks()[0].ID); err != nil {
		t.Errorf("the plain fixture refused a write: %v", err)
	}
}

// Every write refuses on a gated board. Gating only Board.Writable() left the
// store accepting all of them, so the frame said "writes will fail" while the
// card it had just accepted sat in its new lane.
func TestGatedFixtureRefusesEveryWrite(t *testing.T) {
	p := NewGated("board-behind")
	id := p.Board().Tasks()[0].ID
	lane := p.Board().Lanes()[1].Name

	if _, err := p.PersistMove(id, lane, "", ""); err == nil {
		t.Error("PersistMove was accepted on a read-only board")
	}
	if err := p.PersistDone(id); err == nil {
		t.Error("PersistDone was accepted on a read-only board")
	}
	if err := p.PersistFields(id, board.FieldPatch{}); err == nil {
		t.Error("PersistFields was accepted on a read-only board")
	}
	if err := p.PersistBody(id, "x"); err == nil {
		t.Error("PersistBody was accepted on a read-only board")
	}
	if err := p.PersistCheckAdd(id, "x"); err == nil {
		t.Error("PersistCheckAdd was accepted on a read-only board")
	}
	if err := p.PersistCheck(id, 0, true); err == nil {
		t.Error("PersistCheck was accepted on a read-only board")
	}
	if err := p.PersistCheckRm(id, 0); err == nil {
		t.Error("PersistCheckRm was accepted on a read-only board")
	}
	if err := p.PersistCheckReword(id, 0, "x"); err == nil {
		t.Error("PersistCheckReword was accepted on a read-only board")
	}
	if newID, err := p.Add("新規", board.AddOptions{}); err == nil {
		t.Errorf("Add was accepted on a read-only board (created %s)", newID)
	}

	// Reads keep working — a gate refuses writes, it does not blind the board.
	if _, err := p.Query("lane:backlog"); err != nil {
		t.Errorf("a read-only board refused a read: %v", err)
	}
}

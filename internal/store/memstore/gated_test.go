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
// contract of rebuilding a pristine board.)
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
	if err := p.PersistDepAdd(id, id); err == nil {
		t.Error("PersistDepAdd was accepted on a read-only board")
	}
	if err := p.PersistDepRm(id, id); err == nil {
		t.Error("PersistDepRm was accepted on a read-only board")
	}
	if newID, err := p.Add("新規", board.AddOptions{}); err == nil {
		t.Errorf("Add was accepted on a read-only board (created %s)", newID)
	}

	// The epic family too. These are STORE-FIRST, so the fixture really applies
	// them — which makes the gate the only thing standing between a read-only
	// board and an epic edit that lands. A gated board that accepted them would
	// be lying in exactly the direction the gate exists to describe.
	// An INACTIVE box, so the activate assertion below cannot pass just because
	// the fixture already had the flag set.
	box := ""
	for _, e := range p.Board().Epics() {
		if !e.Active {
			box = e.ID
			break
		}
	}
	if box == "" {
		t.Fatal("the fixture has no inactive box to test the activate gate with")
	}
	before := *p.Board().Epic(box)
	goal := "読み取り専用でも通ってしまうゴール"
	if newID, err := p.EpicAdd("新しい箱", board.EpicAddOptions{}); err == nil {
		t.Errorf("EpicAdd was accepted on a read-only board (created %s)", newID)
	}
	if err := p.EpicSet(box, board.EpicPatch{Goal: &goal}); err == nil {
		t.Error("EpicSet was accepted on a read-only board")
	}
	if err := p.EpicActivate(box, ""); err == nil {
		t.Error("EpicActivate was accepted on a read-only board")
	}
	if _, err := p.EpicDeactivate(box); err == nil {
		t.Error("EpicDeactivate was accepted on a read-only board")
	}
	// A VALID pair, and an edge that really exists: `EpicDepAdd(box, box)` is
	// refused for waiting on itself and `EpicDepRm(box, box)` for not being an
	// edge, both regardless of the gate — so those spellings pass while the gate
	// is missing.
	other, depBox, edge := "", "", ""
	for _, e := range p.Board().Epics() {
		if e.ID != box && other == "" {
			other = e.ID
		}
		if len(e.Deps) > 0 && edge == "" {
			depBox, edge = e.ID, e.Deps[0]
		}
	}
	if other == "" || edge == "" {
		t.Fatalf("setup: need a second box (%q) and a box carrying an edge (%q → %q)", other, depBox, edge)
	}
	if err := p.EpicDepAdd(box, other); err == nil {
		t.Error("EpicDepAdd was accepted on a read-only board")
	}
	if err := p.EpicDepRm(depBox, edge); err == nil {
		t.Error("EpicDepRm was accepted on a read-only board")
	}
	// And none of them left a mark: a refusal that half-applied would be worse
	// than one that lands.
	if got := p.Board().Epic(box); got.Goal != before.Goal || got.Active != before.Active ||
		len(got.Deps) != len(before.Deps) {
		t.Errorf("a refused epic write still changed the box:\n got %+v\nwant %+v", *got, before)
	}

	// Reads keep working — a gate refuses writes, it does not blind the board.
	if _, err := p.Query("lane:backlog"); err != nil {
		t.Errorf("a read-only board refused a read: %v", err)
	}
}

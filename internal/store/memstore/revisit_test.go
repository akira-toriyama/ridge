package memstore

import (
	"strings"
	"testing"
)

// Revisit mirrors furrow's RevisitReasons over the fixture: open tasks only,
// reasons in furrow's order with its wording, and -q ANDed on top.
func TestRevisitMirrorsFurrowsSignals(t *testing.T) {
	rows, err := New().Revisit("")
	if err != nil {
		t.Fatal(err)
	}
	by := map[string][]string{}
	for _, r := range rows {
		for _, x := range r.Reasons {
			by[r.ID] = append(by[r.ID], x.Code+"|"+x.Detail)
		}
	}
	// t-jv3j: estimates set, months old, one done dep (t-t38k) — so exactly
	// stale then dep_done, furrow's order.
	got := by["t-jv3j"]
	if len(got) != 2 || !strings.HasPrefix(got[0], "stale|no update in ") || got[1] != "dep_done|dep t-t38k is done" {
		t.Errorf("t-jv3j reasons = %v, want [stale, dep_done t-t38k] in furrow's order", got)
	}
	if !strings.Contains(got[0], "(threshold 30d)") {
		t.Errorf("the stale detail must name the window, got %q", got[0])
	}
	// t-dg7k is the fixture's one draft, and it sits in icebox: furrow skips
	// every TERMINAL lane (done, icebox, waiting), not just done, so the
	// draft signal never gets to fire. Measured on v5.0.0: a flagged task
	// moved to icebox drops out of `revisit` whole.
	if d, ok := by["t-dg7k"]; ok {
		t.Errorf("t-dg7k is in icebox and must not surface, got %v", d)
	}
	for _, r := range rows {
		if st := New().Board().Task(r.ID).Status; terminalLanes[st] {
			t.Errorf("%s surfaced from terminal lane %s", r.ID, st)
		}
	}
	// Done tasks are not eligible, however stale.
	if _, ok := by["t-2qyb"]; ok {
		t.Error("t-2qyb is done and must not surface")
	}
	// The draft signal itself still fires where the lane allows it.
	// (Persist* records nothing here — the move is applied on the board and
	// served through NewWith, the model's own division of labour.)
	b := New().Board()
	if _, err := b.MoveTo("t-dg7k", "backlog", 0); err != nil {
		t.Fatal(err)
	}
	rows, err = NewWith(b).Revisit("")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.ID == "t-dg7k" {
			found = true
			if len(r.Reasons) == 0 || r.Reasons[0].Code != "no_repo" || r.Reasons[0].Detail != "attached to no repo (draft)" {
				t.Errorf("t-dg7k in backlog: reasons = %+v, want no_repo first with furrow's detail", r.Reasons)
			}
		}
	}
	if !found {
		t.Error("t-dg7k moved to backlog must surface as a draft")
	}
}

func TestRevisitANDsTheQueryAndRefusesLikeQuery(t *testing.T) {
	s := New()
	all, err := s.Revisit("")
	if err != nil {
		t.Fatal(err)
	}
	flagged := map[string]bool{}
	for _, r := range all {
		flagged[r.ID] = true
	}
	blocked, err := s.Query("is:blocked")
	if err != nil {
		t.Fatal(err)
	}
	isBlocked := map[string]bool{}
	for _, id := range blocked {
		isBlocked[id] = true
	}
	narrowed, err := s.Revisit("is:blocked")
	if err != nil {
		t.Fatal(err)
	}
	if len(narrowed) == 0 || len(narrowed) >= len(all) {
		t.Fatalf("-q must narrow: %d of %d", len(narrowed), len(all))
	}
	for _, r := range narrowed {
		if !flagged[r.ID] || !isBlocked[r.ID] {
			t.Errorf("%s came back without being both flagged and blocked", r.ID)
		}
	}
	if rows, err := s.Revisit("value:>3"); err == nil || rows != nil {
		t.Errorf("a query the fixture cannot honour must be refused whole, got %d rows, err %v", len(rows), err)
	}
}

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
	// t-dg7k is the fixture's one draft: no_repo leads its reasons.
	if d := by["t-dg7k"]; len(d) == 0 || !strings.HasPrefix(d[0], "no_repo|attached to no repo (draft)") {
		t.Errorf("t-dg7k reasons = %v, want no_repo first", d)
	}
	// Done tasks are not eligible, however stale.
	if _, ok := by["t-2qyb"]; ok {
		t.Error("t-2qyb is done and must not surface")
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

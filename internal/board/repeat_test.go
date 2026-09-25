package board

import (
	"strings"
	"testing"
	"time"
)

func sp(s string) *string { return &s }

// furrow's due/rule coupling and anchor rule, measured on v6.0.0 (CI's pin)
// and dev 2026-09-25: a rule needs a due to count from; a repeating task keeps
// its due until the rule is dropped, the two drops together being accepted;
// every rule write anchors the series at the task's due as of that write —
// `--repeat R` alone included — while a due write alone moves this occurrence
// and never the anchor.
func TestSetFieldsMirrorsFurrowsDueRuleCoupling(t *testing.T) {
	due := time.Date(2026, 10, 2, 14, 59, 59, 0, time.UTC)
	b := NewBoard([]*Task{
		{ID: "a", Status: "ready", Title: "with due", Due: due},
		{ID: "n", Status: "ready", Title: "no due"},
	})

	if err := b.SetFields("a", FieldPatch{Repeat: sp("  weekly  ")}); err != nil {
		t.Fatalf("a rule on a task with a due: %v", err)
	}
	a := b.Task("a")
	if a.Repeat != "weekly" || !a.RepeatAnchor.Equal(due) {
		t.Fatalf("repeat=%q anchor=%v, want the trimmed spelling anchored at the due %v", a.Repeat, a.RepeatAnchor, due)
	}

	// A due write alone moves THIS occurrence; the anchor stays.
	if err := b.SetFields("a", FieldPatch{Due: sp("2026-10-09")}); err != nil {
		t.Fatal(err)
	}
	if a.Due.Equal(due) || !a.RepeatAnchor.Equal(due) {
		t.Errorf("after --due: due=%v anchor=%v, want a moved due and the anchor still at %v", a.Due, a.RepeatAnchor, due)
	}

	// Re-committing the rule re-anchors at the due now carried.
	if err := b.SetFields("a", FieldPatch{Repeat: sp("weekly")}); err != nil {
		t.Fatal(err)
	}
	if !a.RepeatAnchor.Equal(a.Due) {
		t.Errorf("after --repeat alone: anchor=%v, want the current due %v", a.RepeatAnchor, a.Due)
	}

	// Clearing the due under a rule is refused, before anything is touched.
	err := b.SetFields("a", FieldPatch{Due: sp("")})
	if err == nil || !strings.Contains(err.Error(), "must keep a due") {
		t.Errorf("--clear-due on a repeating task = %v, want the keep-a-due refusal", err)
	}
	if a.Due.IsZero() || a.Repeat == "" {
		t.Errorf("the refusal must leave the task untouched: %+v", a)
	}

	// Both drops in one write are accepted.
	if err := b.SetFields("a", FieldPatch{Due: sp(""), Repeat: sp("")}); err != nil {
		t.Fatalf("--clear-due --clear-repeat together: %v", err)
	}
	if !a.Due.IsZero() || a.Repeat != "" || !a.RepeatAnchor.IsZero() {
		t.Errorf("after both drops: %+v, want no due, no rule, no anchor", a)
	}

	// A rule on a task with no due is refused; dropping a rule it never had
	// is a no-op; a due and a rule in ONE write anchor at that due.
	err = b.SetFields("n", FieldPatch{Repeat: sp("weekly")})
	if err == nil || !strings.Contains(err.Error(), "set the due first") {
		t.Errorf("--repeat on a task with no due = %v, want the set-the-due-first refusal", err)
	}
	if n := b.Task("n"); n.Repeat != "" || !n.RepeatAnchor.IsZero() {
		t.Errorf("the refusal must leave the task untouched: %+v", n)
	}
	if err := b.SetFields("n", FieldPatch{Repeat: sp("")}); err != nil {
		t.Errorf("--clear-repeat on a task with no rule: %v, want a no-op", err)
	}
	if err := b.SetFields("n", FieldPatch{Due: sp("2026-11-01"), Repeat: sp("monthly")}); err != nil {
		t.Fatalf("--due --repeat in one write: %v", err)
	}
	if n := b.Task("n"); n.Repeat != "monthly" || n.Due.IsZero() || !n.RepeatAnchor.Equal(n.Due) {
		t.Errorf("after --due --repeat: %+v, want the rule anchored at the new due", n)
	}
}

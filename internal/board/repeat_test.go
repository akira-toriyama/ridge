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

// The rest of furrow's rule refusals (measured on v6.0.0 and dev): a blank
// rule is `--repeat ”`'s exit 2, not a clear — only "" clears, which is the
// port's word; a rule on a closed task could never fire (judged AFTER the
// due: a closed task with no due is answered with the due), while dropping
// one there is exit 0; and the coupling is judged only when the patch
// touches the due or the rule, so a task already carrying a rule with no
// due — a state no furrow write produces — still takes a label.
func TestSetFieldsMirrorsFurrowsOtherRuleRefusals(t *testing.T) {
	due := time.Date(2026, 10, 2, 14, 59, 59, 0, time.UTC)
	closedAt := time.Date(2026, 9, 20, 9, 0, 0, 0, time.UTC)
	b := NewBoard([]*Task{
		{ID: "a", Status: "ready", Title: "with due", Due: due},
		{ID: "c", Status: "done", Title: "closed with due", Due: due, Closed: closedAt},
		{ID: "cn", Status: "done", Title: "closed no due", Closed: closedAt},
		{ID: "odd", Status: "ready", Title: "rule with no due", Repeat: "FREQ=WEEKLY"},
	})

	err := b.SetFields("a", FieldPatch{Repeat: sp("   ")})
	if err == nil || !strings.Contains(err.Error(), "needs a rule") {
		t.Errorf("a whitespace-only rule = %v, want the needs-a-rule refusal, not a clear", err)
	}
	if a := b.Task("a"); a.Repeat != "" || !a.RepeatAnchor.IsZero() {
		t.Errorf("the refusal must leave the task untouched: %+v", a)
	}

	err = b.SetFields("c", FieldPatch{Repeat: sp("weekly")})
	if err == nil || !strings.Contains(err.Error(), "closed") {
		t.Errorf("a rule on a closed task = %v, want the closed refusal", err)
	}
	if c := b.Task("c"); c.Repeat != "" || !c.RepeatAnchor.IsZero() {
		t.Errorf("the refusal must leave the task untouched: %+v", c)
	}
	err = b.SetFields("cn", FieldPatch{Repeat: sp("weekly")})
	if err == nil || !strings.Contains(err.Error(), "set the due first") {
		t.Errorf("a rule on a closed task with no due = %v, want the due named first, as furrow does", err)
	}
	if err := b.SetFields("c", FieldPatch{Repeat: sp("")}); err != nil {
		t.Errorf("--clear-repeat on a closed task: %v, want exit 0", err)
	}

	if err := b.SetFields("odd", FieldPatch{AddLabels: []string{"x"}}); err != nil {
		t.Errorf("a label on a task carrying a rule with no due: %v, want it taken — the coupling is judged only when the patch touches the due or the rule", err)
	}
	if err := b.SetFields("odd", FieldPatch{Due: sp("")}); err == nil {
		t.Error("clearing the (absent) due under the rule must still be the keep-a-due refusal — the patch touched the due")
	}
}

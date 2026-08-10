package memstore

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The fixture's approximation of furrow's -q. The grammar's one definition
// lives furrow-side (t-ehk7) and the real provider passes the string through;
// this evaluator exists ONLY so -dump/-demo and the ui tests stay
// deterministic without a furrow binary. It covers the subset the headless
// frames use and REFUSES — like furrow's exit 2, all-or-nothing — rather
// than mis-evaluating what it does not support (numeric/date comparisons,
// depends-on:/blocks:, is:stale).
//
// Supported: field:value for lane|status|repo|label|id|epic, comma = OR,
// leading - negates, no:/has:, is:actionable|blocked|open|closed|draft|
// unfiled|overdue, bare words over title/id.

// term is one parsed token.
type term struct {
	neg  bool
	key  string // "" = bare word
	vals []string
}

type parsedQuery struct {
	terms    []term
	problems []string // refusals; any entry makes the whole query invalid
}

var queryKeys = map[string]bool{
	"lane": true, "status": true, "repo": true, "label": true,
	"is": true, "no": true, "has": true,
	"id": true, "epic": true,
}

var isValues = map[string]bool{
	"actionable": true, "blocked": true, "open": true, "closed": true,
	"draft": true, "unfiled": true, "overdue": true,
}

var noHasValues = map[string]bool{
	"repo": true, "label": true, "dep": true, "epic": true,
	"body": true, "checklist": true, "value": true, "effort": true,
}

func parseQuery(s string) parsedQuery {
	var q parsedQuery
	for _, tok := range strings.Fields(s) {
		t := term{}
		if strings.HasPrefix(tok, "-") && len(tok) > 1 {
			t.neg = true
			tok = tok[1:]
		}
		k, v, isPair := strings.Cut(tok, ":")
		if !isPair {
			t.vals = []string{strings.ToLower(tok)}
			q.terms = append(q.terms, t)
			continue
		}
		k = strings.ToLower(k)
		if !queryKeys[k] {
			q.problems = append(q.problems, fmt.Sprintf("unknown key %q", k))
			continue
		}
		if v == "" {
			q.problems = append(q.problems, fmt.Sprintf("%s: needs a value", k))
			continue
		}
		if k == "status" {
			k = "lane"
		}
		t.key = k
		for _, part := range strings.Split(v, ",") {
			if part == "" {
				continue
			}
			t.vals = append(t.vals, strings.ToLower(part))
		}
		switch k {
		case "is":
			q.problems = refuseUnknown(t.vals, isValues, q.problems, "is")
		case "no", "has":
			q.problems = refuseUnknown(t.vals, noHasValues, q.problems, k)
		}
		if len(t.vals) > 0 {
			q.terms = append(q.terms, t)
		}
	}
	return q
}

// refuseUnknown records a refusal for every value outside the vocabulary —
// furrow's -q is all-or-nothing, so the fixture refuses too instead of
// silently matching nothing.
func refuseUnknown(vals []string, vocab map[string]bool, problems []string, key string) []string {
	for _, v := range vals {
		if !vocab[v] {
			problems = append(problems,
				fmt.Sprintf("%s:%s is not supported by the fixture filter (%s)", key, v, keyList(vocab)))
		}
	}
	return problems
}

func keyList(m map[string]bool) string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, "|")
}

// match reports whether a task satisfies the whole query: every term must
// hold (a negated term must NOT hold), and within a term any value may.
func (q parsedQuery) match(t *board.Task, g *board.Graph) bool {
	for _, tm := range q.terms {
		if tm.matches(t, g) == tm.neg {
			return false
		}
	}
	return true
}

func (t term) matches(task *board.Task, g *board.Graph) bool {
	for _, v := range t.vals {
		if t.matchOne(task, g, v) {
			return true
		}
	}
	return false
}

func (t term) matchOne(task *board.Task, g *board.Graph, v string) bool {
	switch t.key {
	case "":
		return strings.Contains(strings.ToLower(task.Title), v) ||
			strings.Contains(strings.ToLower(task.ID), v)
	case "lane":
		return strings.ToLower(task.Status) == v
	case "id":
		return strings.ToLower(task.ID) == v
	case "epic":
		return strings.ToLower(task.Epic) == v
	case "repo":
		return containsFold(task.Repos, v)
	case "label":
		return containsFold(task.Labels, v)
	case "is":
		return t.matchIs(task, g, v)
	case "no":
		return !hasField(task, v)
	case "has":
		return hasField(task, v)
	}
	return false
}

func (t term) matchIs(task *board.Task, g *board.Graph, v string) bool {
	switch v {
	case "blocked":
		return len(g.BlockedBy(task.ID)) > 0
	case "actionable":
		return g.Actionable(task.ID)
	case "closed":
		return g.IsDone(task.ID)
	case "open":
		return !g.IsDone(task.ID)
	case "draft":
		return len(task.Repos) == 0
	case "unfiled":
		return task.Epic == ""
	case "overdue":
		return !task.Due.IsZero() && task.Due.Before(time.Now()) && task.Closed.IsZero()
	}
	return false
}

func hasField(t *board.Task, field string) bool {
	switch field {
	case "repo":
		return len(t.Repos) > 0
	case "label":
		return len(t.Labels) > 0
	case "dep":
		return len(t.Deps) > 0
	case "epic":
		return t.Epic != ""
	case "body":
		return strings.TrimSpace(t.Body) != ""
	case "checklist":
		return len(t.Checklist) > 0
	case "value":
		return t.Value > 0
	case "effort":
		return t.Effort > 0
	}
	return false
}

// containsFold reports a case-insensitive substring hit in any element, so
// "repo:vista" matches "akira-toriyama/vista" the way furrow's does.
func containsFold(hay []string, needle string) bool {
	for _, h := range hay {
		if strings.Contains(strings.ToLower(h), needle) {
			return true
		}
	}
	return false
}

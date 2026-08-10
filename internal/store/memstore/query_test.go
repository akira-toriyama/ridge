package memstore

import (
	"sort"
	"strings"
	"testing"
)

// matched runs a query over the whole fixture and returns the ids, sorted.
// It fails the test on a refusal — refusal cases assert via queryErr.
func matched(t *testing.T, q string) []string {
	t.Helper()
	ids, err := New().Query(q)
	if err != nil {
		t.Fatalf("Query(%q) refused: %v", q, err)
	}
	sort.Strings(ids)
	return ids
}

func queryErr(t *testing.T, q string) error {
	t.Helper()
	ids, err := New().Query(q)
	if err == nil {
		t.Fatalf("Query(%q) = %v, want a refusal", q, ids)
	}
	return err
}

func TestParseQueryShapes(t *testing.T) {
	tests := []struct {
		in       string
		terms    int
		problems int
		check    func(t *testing.T, q parsedQuery)
	}{
		{in: "", terms: 0},
		{in: "   ", terms: 0},
		{in: "lane:ready", terms: 1, check: func(t *testing.T, q parsedQuery) {
			if q.terms[0].key != "lane" || q.terms[0].vals[0] != "ready" {
				t.Errorf("got %+v", q.terms[0])
			}
		}},
		{in: "status:ready", terms: 1, check: func(t *testing.T, q parsedQuery) {
			if q.terms[0].key != "lane" {
				t.Errorf("status: must alias to lane, got %q", q.terms[0].key)
			}
		}},
		{in: "-lane:done", terms: 1, check: func(t *testing.T, q parsedQuery) {
			if !q.terms[0].neg {
				t.Error("leading - must negate")
			}
		}},
		{in: "lane:inbox,backlog", terms: 1, check: func(t *testing.T, q parsedQuery) {
			if len(q.terms[0].vals) != 2 {
				t.Errorf("comma is OR within a token, got %v", q.terms[0].vals)
			}
		}},
		{in: "lane:ready repo:vista", terms: 2},
		{in: "filter bar", terms: 2, check: func(t *testing.T, q parsedQuery) {
			if q.terms[0].key != "" {
				t.Error("a bare word must have no key")
			}
		}},
		// Anything the approximation cannot honour is a REFUSAL — furrow's -q
		// is all-or-nothing (exit 2), and a fixture that silently guessed
		// would let a frame lie about what the real store shows.
		{in: "nope:x lane:ready", terms: 1, problems: 1},
		{in: "lane:", terms: 0, problems: 1},
		{in: "is:wat", terms: 1, problems: 1},
		{in: "no:banana", terms: 1, problems: 1},
		{in: "value:>=4", terms: 0, problems: 1}, // real -q syntax the fixture refuses
		{in: "-", terms: 1},                      // a lone dash is a bare word, not a negation
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			q := parseQuery(tc.in)
			if len(q.terms) != tc.terms {
				t.Errorf("terms = %d, want %d (%+v)", len(q.terms), tc.terms, q.terms)
			}
			if len(q.problems) != tc.problems {
				t.Errorf("problems = %d, want %d (%v)", len(q.problems), tc.problems, q.problems)
			}
			if tc.check != nil && len(q.terms) > 0 {
				tc.check(t, q)
			}
		})
	}
}

func TestQueryRefusesRatherThanGuessing(t *testing.T) {
	for _, q := range []string{"nope:x lane:ready", "is:stale", "updated:>=-2w", "depends-on:t-jv3j"} {
		err := queryErr(t, q)
		if !strings.Contains(err.Error(), "not supported") && !strings.Contains(err.Error(), "unknown key") {
			t.Errorf("Query(%q) refusal should say why: %v", q, err)
		}
	}
}

func TestQueryMatchesFixture(t *testing.T) {
	_, g := fixtureGraph(t)

	tests := []struct {
		q    string
		want []string // exact set, or nil to use the checks below
		min  int
	}{
		{q: "lane:ready", want: []string{"t-n2fc"}},
		{q: "lane:in-progress", want: []string{}}, // the epic is an entity, not a card in a lane
		{q: "is:actionable", want: []string{"t-n2fc"}},
		{q: "id:t-jv3j", want: []string{"t-jv3j"}},
		{q: "epic:e-fw2m", min: 18},
		{q: "has:epic", min: 18},
		{q: "is:unfiled", min: 4}, // 23 tasks - 18 members - the epic entity itself is no task
		{q: "repo:vista", min: 21},
		{q: "label:ui", min: 9},
		{q: "no:label", min: 1},
		{q: "no:repo", want: []string{}}, // the fixture has no drafts
		{q: "is:overdue", want: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.q, func(t *testing.T) {
			got := matched(t, tc.q)
			if tc.want != nil {
				if strings.Join(got, ",") != strings.Join(tc.want, ",") {
					t.Errorf("%s = %v, want %v", tc.q, got, tc.want)
				}
				return
			}
			if len(got) < tc.min {
				t.Errorf("%s matched %d, want >= %d", tc.q, len(got), tc.min)
			}
		})
	}

	// is:blocked must agree with the graph, exactly — one definition, shared.
	blocked := matched(t, "is:blocked")
	var fromGraph []string
	b := New().Board()
	for _, task := range b.Tasks() {
		if len(g.BlockedBy(task.ID)) > 0 {
			fromGraph = append(fromGraph, task.ID)
		}
	}
	sort.Strings(fromGraph)
	if strings.Join(blocked, ",") != strings.Join(fromGraph, ",") {
		t.Errorf("is:blocked = %v, graph says %v", blocked, fromGraph)
	}

	// is:closed and is:open partition the board.
	if n := len(matched(t, "is:closed")) + len(matched(t, "is:open")); n != len(b.Tasks()) {
		t.Errorf("closed + open = %d, want %d", n, len(b.Tasks()))
	}
}

func TestQueryAndOrNegation(t *testing.T) {
	// Separate tokens AND.
	both := matched(t, "lane:backlog is:blocked")
	onlyLane := matched(t, "lane:backlog")
	onlyBlocked := matched(t, "is:blocked")
	if len(both) >= len(onlyLane) || len(both) == 0 {
		t.Errorf("AND did not narrow: %d vs %d", len(both), len(onlyLane))
	}
	for _, id := range both {
		if !containsID(onlyLane, id) || !containsID(onlyBlocked, id) {
			t.Errorf("%s matched the conjunction but not both halves", id)
		}
	}

	// Comma ORs within a token.
	or := matched(t, "lane:ready,in-progress")
	if len(or) != len(matched(t, "lane:ready"))+len(matched(t, "lane:in-progress")) {
		t.Errorf("comma OR = %v", or)
	}

	// Negation is the complement.
	all := matched(t, "")
	notDone := matched(t, "-lane:done")
	done := matched(t, "lane:done")
	if len(notDone)+len(done) != len(all) {
		t.Errorf("negation is not the complement: %d + %d != %d", len(notDone), len(done), len(all))
	}
}

func TestQueryBareWordIsCaseInsensitiveSubstring(t *testing.T) {
	if got := matched(t, "KANBAN"); len(got) == 0 {
		t.Error("bare word must be case-insensitive")
	}
	// A CJK bare word has to work: the whole fixture is Japanese titles.
	got := matched(t, "依存")
	if len(got) == 0 {
		t.Fatal("CJK bare word matched nothing")
	}
	for _, id := range got {
		b := New().Board()
		if !strings.Contains(b.Task(id).Title, "依存") {
			t.Errorf("%s does not contain the needle", id)
		}
	}
	// A bare word also matches an id, which is how you paste one in.
	if got := matched(t, "t-jv3j"); len(got) != 1 || got[0] != "t-jv3j" {
		t.Errorf("bare id = %v", got)
	}
}

func containsID(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

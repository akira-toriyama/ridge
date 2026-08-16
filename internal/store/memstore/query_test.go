package memstore

import (
	"sort"
	"strings"
	"testing"
	"time"
)

// The expectations in this file are FURROW's, measured against a real store
// on 2026-08-10 (t-74y3) and against `furrow vocab query-is` /
// `query-presence` / `query-qualifiers` for the vocabularies. Where a case
// records what furrow does, the comment says so — nothing here is derived
// from query.go's own expression, because a test written that way passes for
// any implementation.

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

// fixtureVocab is the lane/repo vocabulary parseQuery validates against.
func fixtureVocab() queryVocab { return boardVocab(New().Board()) }

// ids spells an expected set inline so an assertion states the answer rather
// than recomputing it.
func ids(s ...string) string {
	sort.Strings(s)
	return strings.Join(s, ",")
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
		{in: "lane:ready repo:kyushu-trip", terms: 2, check: func(t *testing.T, q parsedQuery) {
			// A short name resolves to the full repo AT PARSE TIME, the way
			// furrow resolves -r / repo: before it filters.
			if got := q.terms[1].vals[0]; got != "tomo/kyushu-trip" {
				t.Errorf("repo:kyushu-trip must resolve to the full name, got %q", got)
			}
		}},
		{in: "filter bar", terms: 2, check: func(t *testing.T, q parsedQuery) {
			if q.terms[0].key != "" {
				t.Error("a bare word must have no key")
			}
		}},
		// Quotes hold a phrase together — one term, not two.
		{in: `"filter bar"`, terms: 1, check: func(t *testing.T, q parsedQuery) {
			if q.terms[0].key != "" || q.terms[0].vals[0] != "filter bar" {
				t.Errorf("a quoted phrase is one bare-word term, got %+v", q.terms[0])
			}
		}},
		{in: `'filter bar'`, terms: 1},
		// Measured: a token that OPENS with a quote is free text even when it
		// holds a colon — `"lane:inbox"` is a phrase, not a lane term.
		{in: `"lane:inbox"`, terms: 1, check: func(t *testing.T, q parsedQuery) {
			if q.terms[0].key != "" {
				t.Errorf("a fully quoted token is free text, got key %q", q.terms[0].key)
			}
		}},
		// A quote AFTER the colon means "the whole field" (title:).
		{in: `title:"filter bar"`, terms: 1, check: func(t *testing.T, q parsedQuery) {
			if q.terms[0].key != "title" || !q.terms[0].exact {
				t.Errorf("a quoted value must be marked exact, got %+v", q.terms[0])
			}
		}},
		{in: `title:filter`, terms: 1, check: func(t *testing.T, q parsedQuery) {
			if q.terms[0].exact {
				t.Error("an unquoted title: is a substring, not the whole field")
			}
		}},
		// Vocabulary furrow has and the fixture now honours.
		{in: "no:deps", terms: 1},
		{in: "no:refs no:due no:closed no:reviewed", terms: 4},
		{in: "is:stale", terms: 1},
		{in: "body:kanban", terms: 1},
		// Anything the fixture cannot honour is a REFUSAL — furrow's -q is
		// all-or-nothing (exit 2), and a fixture that silently guessed would
		// let a frame lie about what the real store shows.
		{in: "nope:x lane:ready", terms: 1, problems: 1},
		{in: "lane:", terms: 0, problems: 1},
		{in: "is:wat", terms: 1, problems: 1},
		{in: "no:banana", terms: 1, problems: 1},
		{in: "no:dep", terms: 1, problems: 1},    // furrow spells it deps
		{in: "value:>=4", terms: 0, problems: 1}, // real -q syntax the fixture refuses
		{in: "depends-on:t-jv3j", terms: 0, problems: 1},
		{in: "lane:nope", terms: 1, problems: 1},     // furrow: exit 2 unknown-lane
		{in: "repo:vis", terms: 1, problems: 1},      // furrow: exit 2 repo-unknown
		{in: `"unterminated`, terms: 0, problems: 1}, // furrow: exit 2 query-parse
		{in: "-", terms: 1},                          // a lone dash is a bare word, not a negation
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			q := parseQuery(tc.in, fixtureVocab())
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
	// Left column: what furrow does with the same string. Every one of these
	// is a refusal here because the fixture cannot honour it — never a quiet
	// zero, which is the failure mode this whole file exists to prevent.
	for _, q := range []string{
		"nope:x lane:ready", // exit 2 query-unknown-field
		"updated:>=-2w",     // real qualifier, date comparison
		"depends-on:t-jv3j", // real qualifier, graph traversal
		"value:>=4",         // real qualifier, ordinal comparison
		"no:dep",            // exit 2 query-unknown-field: furrow has deps
		"lane:nope",         // exit 2 unknown-lane
		"repo:vis",          // exit 2 repo-unknown
	} {
		err := queryErr(t, q)
		if !strings.Contains(err.Error(), "not supported") &&
			!strings.Contains(err.Error(), "unknown key") &&
			!strings.Contains(err.Error(), "unknown lane") &&
			!strings.Contains(err.Error(), "no known repo") {
			t.Errorf("Query(%q) refusal should say why: %v", q, err)
		}
	}
}

func TestQueryMatchesFixture(t *testing.T) {
	// The clock-dependent predicates (is:overdue here) are pinned to the day
	// this file's expectations were measured — the fixture's dues are fixed
	// instants, so a wall clock would walk tasks across the overdue line.
	prev := nowFn
	nowFn = func() time.Time { return time.Date(2026, 8, 10, 0, 0, 0, 0, time.UTC) }
	t.Cleanup(func() { nowFn = prev })

	tests := []struct {
		q    string
		want []string // exact set, or nil to use the min below
		min  int
	}{
		{q: "lane:ready", want: []string{"t-n2fc"}},
		{q: "lane:in-progress", want: []string{"t-c9dm", "t-e5hq", "t-kv82", "t-w3np"}}, // the prep line in flight
		{q: "is:actionable", want: []string{"t-c9dm", "t-e5hq", "t-kv82", "t-n2fc", "t-w3np"}},
		{q: "id:t-jv3j", want: []string{"t-jv3j"}},
		{q: "epic:e-fw2m", min: 18},
		{q: "has:epic", min: 18},
		{q: "is:unfiled", min: 15}, // 33 tasks - 18 members - the epic entity itself is no task
		{q: "repo:kyushu-trip", min: 27},
		{q: "label:bbq", min: 9},
		{q: "no:label", min: 1},
		{q: "no:repo", want: []string{}}, // the fixture has no drafts
		// Of the fixture's three dues, only t-jv3j (2026-07-31) is past the
		// pinned clock and still open.
		{q: "is:overdue", want: []string{"t-jv3j"}},
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
}

// The blocked/done sets are FIXTURE FACTS, named here as literals. The
// earlier version of this test rebuilt them from board.Graph — the same
// expression is:blocked evaluates — so it agreed with the implementation by
// construction and could not fail.
func TestQueryIsBlockedNamesTheBlockedTasks(t *testing.T) {
	// t-jv3j is blocked by the open t-ehk7 (fixture_test.go pins that edge);
	// the other four each still wait on an open dep of their own.
	want := ids("t-0esb", "t-jv3j", "t-pk4f", "t-px9p", "t-rmtc")
	if got := strings.Join(matched(t, "is:blocked"), ","); got != want {
		t.Errorf("is:blocked = %v, want %v", got, want)
	}
	// And the complement is not merely "everything else": a task with only
	// DONE deps is not blocked. t-r7wr and t-9m2q both carry deps.
	for _, id := range []string{"t-n2fc", "t-r7wr", "t-9m2q"} {
		if containsID(matched(t, "is:blocked"), id) {
			t.Errorf("%s has no open dep; it must not be blocked", id)
		}
	}
}

func TestQueryIsClosedAndIsOpenNameTheirTasks(t *testing.T) {
	// The nine done cards, spelled out. The earlier version asserted
	// len(closed)+len(open) == len(tasks), which holds for ANY predicate —
	// `return true` and `return false` both passed it.
	wantClosed := ids("t-2qyb", "t-2tbn", "t-614w", "t-6etg", "t-ecfm",
		"t-g8bn", "t-phgp", "t-t38k", "t-wf4p")
	if got := strings.Join(matched(t, "is:closed"), ","); got != wantClosed {
		t.Errorf("is:closed = %v, want %v", got, wantClosed)
	}
	// A representative slice of the open side, named rather than counted.
	open := matched(t, "is:open")
	for _, id := range []string{"t-n2fc", "t-jv3j", "t-ehk7", "t-7mcc", "t-fn4k"} {
		if !containsID(open, id) {
			t.Errorf("is:open is missing %s", id)
		}
	}
	for _, id := range []string{"t-t38k", "t-wf4p"} {
		if containsID(open, id) {
			t.Errorf("is:open must not hold the done task %s", id)
		}
	}
	// has:closed is the same set by a different road — furrow stamps Closed
	// when a task enters the done lane.
	if got := strings.Join(matched(t, "has:closed"), ","); got != wantClosed {
		t.Errorf("has:closed = %v, want %v", got, wantClosed)
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
	if got := matched(t, "BBQ"); len(got) == 0 {
		t.Error("bare word must be case-insensitive")
	}
	// A CJK bare word has to work: the whole fixture is Japanese titles.
	got := matched(t, "天地返し")
	if len(got) == 0 {
		t.Fatal("CJK bare word matched nothing")
	}
	for _, id := range got {
		b := New().Board()
		if !strings.Contains(b.Task(id).Title, "天地返し") {
			t.Errorf("%s does not contain the needle", id)
		}
	}
	// A bare word also matches an id, which is how you paste one in.
	if got := matched(t, "t-jv3j"); len(got) != 1 || got[0] != "t-jv3j" {
		t.Errorf("bare id = %v", got)
	}
}

// The label/repo/epic/id matching rules had NOTHING pinning them: flipping
// the comparison from substring to exact left the whole suite green, which is
// how the fixture drifted into answering `label:u` with nine cards furrow
// answers with none. Every expectation below was measured against a real
// furrow store on 2026-08-10.
func TestQueryLabelIsExactAndCaseSensitive(t *testing.T) {
	full := matched(t, "label:bbq")
	if len(full) < 9 {
		t.Fatalf("label:bbq matched %d, want the 9 bbq-tagged cards", len(full))
	}
	// A prefix is NOT a match — this is the substring regression's tripwire.
	if got := matched(t, "label:b"); len(got) != 0 {
		t.Errorf("label:b must match nothing (furrow: exact), got %v", got)
	}
	if got := matched(t, "label:q"); len(got) != 0 {
		t.Errorf("label:q must match nothing (furrow: exact), got %v", got)
	}
	// And the case must match byte-for-byte: the fixture tags are lowercase.
	if got := matched(t, "label:BBQ"); len(got) != 0 {
		t.Errorf("label:BBQ must match nothing (furrow: case-sensitive), got %v", got)
	}
	if got := matched(t, "label:Bbq"); len(got) != 0 {
		t.Errorf("label:Bbq must match nothing (furrow: case-sensitive), got %v", got)
	}
	// An unknown label is an honest zero, not a refusal — furrow returns 0
	// rows for a label nobody uses.
	if got := matched(t, "label:no-such-label"); len(got) != 0 {
		t.Errorf("an unused label matched %v", got)
	}
}

func TestQueryEpicAndIDMatching(t *testing.T) {
	full := matched(t, "epic:e-fw2m")
	if len(full) != 18 {
		t.Fatalf("epic:e-fw2m matched %d, want 18", len(full))
	}
	// epic: is an exact, case-sensitive id — not a prefix and not a title.
	for _, q := range []string{"epic:e-fw", "epic:E-FW2M", "epic:kyushu"} {
		if got := matched(t, q); len(got) != 0 {
			t.Errorf("%s must match nothing (furrow: exact id), got %d", q, len(got))
		}
	}
	// id: IS a prefix, case-sensitive (measured: id:t-hs found t-hsgek,
	// id:T-HSGEK found nothing).
	if got := matched(t, "id:t-jv"); len(got) != 1 || got[0] != "t-jv3j" {
		t.Errorf("id:t-jv = %v, want [t-jv3j]", got)
	}
	if got := matched(t, "id:t-"); len(got) != len(matched(t, "")) {
		t.Errorf("id:t- = %d, want the whole board", len(got))
	}
	if got := matched(t, "id:T-JV3J"); len(got) != 0 {
		t.Errorf("id: is case-sensitive; id:T-JV3J = %v", got)
	}
}

func TestQueryRepoResolvesInsteadOfSubstringMatching(t *testing.T) {
	full := matched(t, "repo:tomo/kyushu-trip")
	short := matched(t, "repo:kyushu-trip")
	if len(full) != 27 {
		t.Fatalf("repo:tomo/kyushu-trip matched %d, want 27", len(full))
	}
	if strings.Join(short, ",") != strings.Join(full, ",") {
		t.Errorf("the short name must resolve to the same set: %v vs %v", short, full)
	}
	// Resolution folds case (measured: repo:Vista matched), but it is
	// resolution, NOT a substring search.
	if got := matched(t, "repo:KYUSHU-TRIP"); strings.Join(got, ",") != strings.Join(full, ",") {
		t.Errorf("repo:KYUSHU-TRIP = %v, want the kyushu-trip set", got)
	}
	// A prefix of a real repo resolves to nothing, so furrow refuses it with
	// candidates rather than quietly filtering on a guess.
	for _, q := range []string{"repo:kyushu", "repo:trip", "repo:tomo"} {
		err := queryErr(t, q)
		if !strings.Contains(err.Error(), "no known repo") {
			t.Errorf("%s refusal should name the problem: %v", q, err)
		}
		if !strings.Contains(err.Error(), "tomo/kyushu-trip") {
			t.Errorf("%s refusal should list the candidates: %v", q, err)
		}
	}
	// The other repo on the board is reachable the same two ways.
	if got := matched(t, "repo:joubisai"); len(got) != 7 {
		t.Errorf("repo:joubisai = %v, want the 7 joubisai-tagged cards", got)
	}
}

func TestQueryRefusesAnUnknownLane(t *testing.T) {
	// furrow exits 2 (unknown-lane) with the configured lanes as candidates;
	// the fixture used to answer 0 rows, which reads as "nothing matches"
	// when the truth is "that lane does not exist".
	for _, q := range []string{"lane:nope", "status:nope", "lane:backlog,nope", "-lane:nope"} {
		err := queryErr(t, q)
		if !strings.Contains(err.Error(), "unknown lane") {
			t.Errorf("%s should refuse as an unknown lane: %v", q, err)
		}
		if !strings.Contains(err.Error(), "in-progress") {
			t.Errorf("%s refusal should list the configured lanes: %v", q, err)
		}
	}
	// The comparison is case-sensitive, so a shouted lane is unknown too.
	if err := queryErr(t, "lane:INBOX"); !strings.Contains(err.Error(), "unknown lane") {
		t.Errorf("lane:INBOX should refuse: %v", err)
	}
	// A real lane still works, including the hyphenated one.
	for _, q := range []string{"lane:inbox", "lane:in-progress", "lane:icebox"} {
		matched(t, q)
	}
}

func TestQueryPresenceVocabularyIsFurrows(t *testing.T) {
	all := len(matched(t, ""))
	// `furrow vocab query-presence`, every member, over the fixture's facts.
	cases := []struct {
		field    string
		has, not int
	}{
		{"deps", 12, 21},
		{"refs", 0, all},
		{"due", 4, all - 4},
		{"closed", 9, all - 9},
		{"reviewed", 0, all},
		{"label", 18, 15},
		{"repo", all, 0},
		{"epic", 18, all - 18},
		{"checklist", 8, all - 8},
		// value/effort/body are presence fields too. They used to be "covered"
		// by a `has + no == all` check below, which is the exact tautology this
		// file condemns elsewhere: `no:` is the literal negation of `has:`, so
		// it holds for any predicate at all, including a broken one.
		{"value", 32, 1},
		{"effort", 32, 1},
		{"body", 33, 0},
	}
	for _, tc := range cases {
		t.Run(tc.field, func(t *testing.T) {
			if got := matched(t, "has:"+tc.field); len(got) != tc.has {
				t.Errorf("has:%s = %d, want %d", tc.field, len(got), tc.has)
			}
			if got := matched(t, "no:"+tc.field); len(got) != tc.not {
				t.Errorf("no:%s = %d, want %d", tc.field, len(got), tc.not)
			}
		})
	}
	// The singular `dep` is a field the fixture INVENTED — furrow exits 2
	// query-unknown-field on it, and the refusal must name the real spelling.
	err := queryErr(t, "no:dep")
	if !strings.Contains(err.Error(), "deps") {
		t.Errorf("no:dep refusal should point at deps: %v", err)
	}
}

func TestQueryHonoursAQuotedPhrase(t *testing.T) {
	// Measured: furrow answers the phrase; the fixture used to leave the
	// quotes in the needle and answer 0.
	phrase := matched(t, `"行程表 v2"`)
	if len(phrase) != 1 || phrase[0] != "t-jv3j" {
		t.Errorf(`"行程表 v2" = %v, want [t-jv3j]`, phrase)
	}
	if got := matched(t, `'行程表 v2'`); strings.Join(got, ",") != "t-jv3j" {
		t.Errorf("single quotes must quote too, got %v", got)
	}
	// The phrase is STRICTLY narrower than the same words ANDed: two bare
	// words each match a card the phrase does not.
	loose := matched(t, "BBQ サイト")
	tight := matched(t, `"BBQ サイト"`)
	if len(loose) != 2 || len(tight) != 1 {
		t.Errorf("two ANDed words = %v, the phrase = %v; want 2 and 1", loose, tight)
	}
	// A quoted token that holds a colon is free text, not a qualifier.
	if got := matched(t, `"lane:inbox"`); len(got) != 0 {
		t.Errorf(`"lane:inbox" is a phrase nobody's title holds, got %v`, got)
	}
	// A quote inside a phrase-carrying query still ANDs with a qualifier.
	if got := matched(t, `lane:backlog "行程表 v2"`); len(got) != 1 {
		t.Errorf("a phrase must AND with a qualifier, got %v", got)
	}
	// An unterminated quote is a refusal, not a silent half-token.
	if err := queryErr(t, `"行程表 v2`); !strings.Contains(err.Error(), "unterminated quote") {
		t.Errorf("an unterminated quote should say so: %v", err)
	}
}

func TestQueryTitleAndBodyQualifiers(t *testing.T) {
	// title: is a case-insensitive substring...
	if got := matched(t, "title:bbq"); len(got) != 2 {
		t.Errorf("title:bbq = %v, want the two BBQ-titled cards", got)
	}
	if got := matched(t, "title:BBQ"); len(got) != 2 {
		t.Errorf("title: must fold case, got %v", got)
	}
	// ...but QUOTED it means the whole title (measured: title:"filter"
	// matched nothing while title:<the full title> matched).
	b := New().Board()
	full := b.Task("t-t38k").Title
	if got := matched(t, `title:"`+full+`"`); len(got) != 1 || got[0] != "t-t38k" {
		t.Errorf("a quoted title must match the whole field, got %v", got)
	}
	if got := matched(t, `title:"bbq"`); len(got) != 0 {
		t.Errorf(`title:"bbq" is not the whole title; got %v`, got)
	}
	// body: searches the body, and finds what title: cannot.
	body := matched(t, "body:dutch-oven")
	if len(body) == 0 {
		t.Fatal("body: matched nothing in a fixture whose bodies are real")
	}
	for _, id := range body {
		if !strings.Contains(strings.ToLower(b.Task(id).Body), "dutch-oven") {
			t.Errorf("%s body does not hold the needle", id)
		}
	}
	// title: and body: are ANDable with everything else.
	if got := matched(t, "lane:done title:bbq"); len(got) != 1 {
		t.Errorf("title: must AND with a lane, got %v", got)
	}
}

func TestQueryIsStaleUsesFurrowsWindow(t *testing.T) {
	// furrow's [revisit].stale_days default is 30, and the measurement showed
	// is:stale is the update window ALONE — a done task went stale too.
	defer func(f func() time.Time) { nowFn = f }(nowFn)

	// The fixture was snapshotted 2026-07-16..17. A clock just past it makes
	// nothing stale...
	nowFn = func() time.Time { return time.Date(2026, 7, 20, 0, 0, 0, 0, time.UTC) }
	if got := matched(t, "is:stale"); len(got) != 0 {
		t.Errorf("nothing is 30 days old on 2026-07-20, got %v", got)
	}
	// ...and a clock well past it makes everything stale, done cards included.
	nowFn = func() time.Time { return time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC) }
	stale := matched(t, "is:stale")
	if len(stale) != len(matched(t, "")) {
		t.Errorf("is:stale = %d on 2026-09-30, want the whole board", len(stale))
	}
	if !containsID(stale, "t-t38k") {
		t.Error("is:stale must include a DONE task: furrow's window ignores the lane")
	}
	// The boundary: 30 days is not yet stale, 31 is.
	nowFn = func() time.Time { return time.Date(2026, 8, 15, 0, 0, 0, 0, time.UTC) }
	if got := matched(t, "is:stale"); len(got) != 0 {
		t.Errorf("30 days is inside the window, got %v", got)
	}
	nowFn = func() time.Time { return time.Date(2026, 8, 17, 0, 0, 0, 0, time.UTC) }
	if got := matched(t, "is:stale"); len(got) == 0 {
		t.Error("32 days is outside the window; is:stale should have fired")
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

// The slice panel issues quoted values (`label:"needs review"`) that real
// furrow understands. The pre-rewrite lexer had no quote pairing, so PR #12
// taught it to REFUSE quoted values rather than mis-lex them into a silently
// blank -dump frame. This lexer pairs quotes at furrow's measured semantics,
// which supersedes that refusal: the same input now evaluates — exactly —
// and the original contract ("refuse, never mis-evaluate") still holds.
func TestQuotedValuesEvaluateInsteadOfRefusing(t *testing.T) {
	s := New()
	got, err := s.Query(`label:"needs review"`)
	if err != nil {
		t.Fatalf("a quoted label must evaluate now that quotes pair: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("no fixture task carries that label — want an honest 0 rows, got %v", got)
	}
	// Quoting must not change what an existing label matches: label: is
	// exact either way.
	quoted, err := s.Query(`label:"bbq"`)
	if err != nil {
		t.Fatalf(`label:"bbq": %v`, err)
	}
	bare, err := s.Query("label:bbq")
	if err != nil {
		t.Fatalf("label:bbq: %v", err)
	}
	if strings.Join(quoted, ",") != strings.Join(bare, ",") {
		t.Errorf("quoted vs bare label diverged: %v vs %v", quoted, bare)
	}
}

// The comma/quote interactions, each measured against the real binary
// (2026-08-10, t-74y3 review round): quoting suppresses the OR split, the
// split happens OUTSIDE quotes, and the misplaced-quote and empty-value
// shapes refuse the way furrow exits 2 — never a term that silently matches
// everything or nothing.
func TestQuoteCommaInteractionsMatchFurrow(t *testing.T) {
	s := New()
	all := len(mustQuery(t, s, ""))

	// label:"a,b" is ONE alternative holding a literal comma — no fixture
	// label carries a comma, so an honest 0 rows; label:ui,cli is an OR.
	if got := mustQuery(t, s, `label:"bbq,bento"`); len(got) != 0 {
		t.Errorf(`label:"bbq,bento" must be one comma-holding label, got %v`, got)
	}
	orSet := mustQuery(t, s, "label:bbq,bento")
	if len(orSet) == 0 {
		t.Fatal("setup: label:bbq,bento must match")
	}
	// The split runs outside quotes: label:"ui","cli" is the same OR.
	if got := mustQuery(t, s, `label:"bbq","bento"`); strings.Join(got, ",") != strings.Join(orSet, ",") {
		t.Errorf(`label:"bbq","bento" = %v, want the label:bbq,bento set %v`, got, orSet)
	}
	// A quoted comma matches nothing but refuses nothing.
	if got := mustQuery(t, s, `label:","`); len(got) != 0 {
		t.Errorf(`label:"," = %v, want honest 0 rows`, got)
	}
	// A trailing empty alternative drops; the value must not become
	// match-everything.
	if got := mustQuery(t, s, "label:bbq,"); len(got) == 0 || len(got) == all {
		t.Errorf("label:bbq, = %d rows, want the bbq set (all=%d)", len(got), all)
	}

	// The refusal shapes, each exit 2 in furrow.
	for _, q := range []string{
		`""`, `''`, // empty term
		"label:,", `label:""`, // needs a value
		`label:a"b"`, `label:"a"b`, `nee"dle"`, `"label":a`, // mismatched / unknown key
		`is:"open"`, `no:"deps"`, // flags are unquoted
	} {
		if _, err := s.Query(q); err == nil {
			t.Errorf("%s must refuse (furrow: exit 2), and it evaluated", q)
		}
	}

	// An unknown FULL owner/repo form is an honest empty set (measured),
	// unlike the short form, which refuses.
	if got := mustQuery(t, s, "repo:no/such"); len(got) != 0 {
		t.Errorf("repo:no/such = %v, want honest 0 rows", got)
	}
	if _, err := s.Query("repo:nosuch"); err == nil {
		t.Error("a short repo name resolving to nothing must refuse")
	}
}

func mustQuery(t *testing.T, s *Store, q string) []string {
	t.Helper()
	got, err := s.Query(q)
	if err != nil {
		t.Fatalf("Query(%q): %v", q, err)
	}
	return got
}

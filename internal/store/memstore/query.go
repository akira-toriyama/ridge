package memstore

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The fixture's stand-in for furrow's -q. The grammar's one definition lives
// furrow-side (t-ehk7) and the real provider passes the string through; this
// evaluator exists ONLY so -dump/-demo and the ui tests stay deterministic
// without a furrow binary.
//
// Every vocabulary below is a verbatim copy of what `furrow vocab` prints
// (query-is, query-presence, query-qualifiers), and every matching rule was
// measured against a real store — 2026-08-10, t-74y3. That measurement is the
// point: an approximation nobody checks drifts into inventing fields
// (`no:dep`, which furrow has never had) and into guessing (`label:u` as a
// substring, `lane:nope` as a silent zero) — and then a headless frame shows a
// board the real store would never show.
//
// Supported, at furrow's semantics:
//
//	lane:/status:  exact and case-SENSITIVE; an unknown lane REFUSES the whole
//	               query, the way furrow exits 2 unknown-lane with candidates
//	repo:          owner/repo or the short name after the slash, compared
//	               case-insensitively; one that resolves to no repo on the
//	               board REFUSES (furrow: exit 2 repo-unknown). NOT a substring
//	label:/epic:   exact, case-sensitive; an unknown one is an honest 0 rows
//	id:            case-sensitive PREFIX — id:t-jv finds t-jv3j
//	title:         case-insensitive substring; QUOTED it means the whole title
//	body:          case-insensitive substring
//	is:            the whole query-is vocabulary, is:stale included
//	no:/has:       the whole query-presence vocabulary
//	bare words     case-insensitive substring; comma ORs inside a token, a
//	               leading - negates, and quotes hold a phrase together
//
// Two divergences that are DELIBERATE, stated here so a green frame is never
// mistaken for proof of parity:
//
//   - a bare word searches title+ID where furrow searches title+body. The ID
//     leg is ridge's own affordance — pasting an id into the filter bar.
//   - is:stale and is:overdue read the wall clock. furrow reads the board's
//     [revisit].stale_days; the fixture has no config, so furrow's default of
//     30 days is hard-coded here.
//
// Everything else REFUSES — loudly and all-or-nothing, like furrow's exit 2 —
// rather than mis-evaluating it: the ordinal/date comparisons (value:>=4,
// updated:>=-2w) and the graph qualifiers (depends-on:, blocks:,
// descendant-of:, ancestor-of:). Refusing is the contract, not a gap in it: a
// fixture that guessed would let a frame lie about what the real store shows.

// staleDays is furrow's [revisit].stale_days default — the window is:stale
// measures against. The fixture reads no config, so the default is the value.
const staleDays = 30

// nowFn is indirected so the clock-dependent predicates (is:stale,
// is:overdue) are testable, matching board's own test clock.
var nowFn = time.Now

// term is one parsed token.
type term struct {
	neg   bool
	key   string // "" = bare word
	vals  []string
	exact bool // the value was quoted — title: then means the WHOLE title
}

type parsedQuery struct {
	terms    []term
	problems []string // refusals; any entry makes the whole query invalid
}

// queryKeys is what this evaluator can honour. `is`, `no` and `has` are
// qualifiers furrow accepts but leaves out of `furrow vocab
// query-qualifiers`, which lists only the field-shaped ones.
var queryKeys = map[string]bool{
	"lane": true, "status": true, "repo": true, "label": true,
	"is": true, "no": true, "has": true,
	"id": true, "epic": true, "title": true, "body": true,
}

// realButUnsupported are real furrow qualifiers (`furrow vocab
// query-qualifiers`) this evaluator cannot honour over the fixture. They earn
// a refusal that says so, rather than the "unknown key" a typo gets — a user
// who typed `value:>=4` spelled it right and deserves to be told which side
// is missing it.
var realButUnsupported = map[string]string{
	"value": "ordinal comparison", "effort": "ordinal comparison",
	"priority": "ordinal comparison", "roi": "ordinal comparison",
	"created": "date comparison", "updated": "date comparison",
	"closed": "date comparison", "reviewed": "date comparison",
	"due": "date comparison", "depends-on": "graph traversal",
	"blocks": "graph traversal", "descendant-of": "graph traversal",
	"ancestor-of": "graph traversal",
}

// isValues is `furrow vocab query-is`, verbatim.
var isValues = map[string]bool{
	"actionable": true, "blocked": true, "stale": true, "open": true,
	"closed": true, "draft": true, "unfiled": true, "overdue": true,
}

// presenceValues is `furrow vocab query-presence`, verbatim — the vocabulary
// no:/has: take. Note the plurals: furrow has `deps` and `refs`, never `dep`.
var presenceValues = map[string]bool{
	"label": true, "repo": true, "epic": true, "value": true,
	"effort": true, "deps": true, "refs": true, "checklist": true,
	"closed": true, "reviewed": true, "body": true, "due": true,
}

// queryVocab is the board-derived half of the grammar. furrow validates a
// lane and a repo name at PARSE time and exits 2 on one it cannot resolve, so
// the fixture needs the same two sets to refuse in the same places.
type queryVocab struct {
	lanes []string // in board order, as furrow lists its configured lanes
	repos []string // the full owner/repo names the board carries, sorted
}

func boardVocab(b *board.Board) queryVocab {
	var v queryVocab
	for _, l := range b.Lanes() {
		v.lanes = append(v.lanes, l.Name)
	}
	seen := map[string]bool{}
	for _, t := range b.Tasks() {
		for _, r := range t.Repos {
			if !seen[r] {
				seen[r] = true
				v.repos = append(v.repos, r)
			}
		}
	}
	sort.Strings(v.repos)
	return v
}

func (v queryVocab) knownLane(s string) bool {
	for _, l := range v.lanes {
		if l == s {
			return true
		}
	}
	return false
}

// resolveRepo maps a query value onto a repo the board actually carries.
// furrow takes owner/repo or a UNIQUE short name and refuses anything else
// with candidates, so the second return is the refusal text ("" = resolved).
func (v queryVocab) resolveRepo(s string) (string, string) {
	for _, r := range v.repos {
		if strings.EqualFold(r, s) {
			return r, ""
		}
	}
	var hits []string
	for _, r := range v.repos {
		if strings.EqualFold(shortRepo(r), s) {
			hits = append(hits, r)
		}
	}
	switch len(hits) {
	case 1:
		return hits[0], ""
	case 0:
		return "", fmt.Sprintf("repo %q matches no known repo; use the full owner/repo form (known: %s)",
			s, strings.Join(v.repos, ", "))
	default:
		return "", fmt.Sprintf("repo %q is ambiguous (%s)", s, strings.Join(hits, ", "))
	}
}

func shortRepo(r string) string {
	if i := strings.LastIndex(r, "/"); i >= 0 {
		return r[i+1:]
	}
	return r
}

// rawToken is one whitespace-separated token with its quotes stripped, plus
// what the quoting MEANT. furrow reads a token that OPENS with a quote as
// free text — `"lane:inbox"` searches for that literal string and is not a
// lane term — and a quote after the colon as "match the whole field".
type rawToken struct {
	text    string
	literal bool // opened quoted: free text even when it holds a colon
	exact   bool // quoted after the colon
}

// tokenize splits on whitespace OUTSIDE quotes. Both quote characters work,
// and an unterminated one is a refusal (furrow: exit 2 query-parse).
func tokenize(s string) ([]rawToken, error) {
	var (
		toks  []rawToken
		cur   strings.Builder
		tok   rawToken
		open  bool
		quote byte
	)
	colon := -1
	flush := func() {
		if !open {
			return
		}
		tok.text = cur.String()
		if tok.text != "" {
			toks = append(toks, tok)
		}
		cur.Reset()
		tok, open, colon = rawToken{}, false, -1
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			if c == quote {
				quote = 0
				continue
			}
			cur.WriteByte(c)
			continue
		}
		switch c {
		case '"', '\'':
			quote, open = c, true
			if colon < 0 {
				tok.literal = true
			} else {
				tok.exact = true
			}
		case ' ', '\t', '\n', '\r':
			flush()
		default:
			if c == ':' && colon < 0 {
				colon = cur.Len()
			}
			cur.WriteByte(c)
			open = true
		}
	}
	if quote != 0 {
		return nil, fmt.Errorf("unterminated quote in %q", s)
	}
	flush()
	return toks, nil
}

// caseSensitiveKeys are the fields furrow compares byte-for-byte, so their
// values must survive parsing with their case intact — `lane:INBOX` is an
// unknown lane and `label:UI` is a different label from `label:ui`. Every
// other value (is:/no:/has:, title:, body: and a bare word) is folded here
// once, because its comparison folds anyway.
var caseSensitiveKeys = map[string]bool{
	"lane": true, "label": true, "epic": true, "id": true, "repo": true,
}

func parseQuery(s string, v queryVocab) parsedQuery {
	var q parsedQuery
	toks, err := tokenize(s)
	if err != nil {
		q.problems = append(q.problems, err.Error())
		return q
	}
	for _, rt := range toks {
		t := term{exact: rt.exact}
		text := rt.text
		if strings.HasPrefix(text, "-") && len(text) > 1 {
			t.neg = true
			text = text[1:]
		}
		k, val, isPair := strings.Cut(text, ":")
		if rt.literal || !isPair {
			t.vals = []string{strings.ToLower(text)}
			q.terms = append(q.terms, t)
			continue
		}
		k = strings.ToLower(k)
		if !queryKeys[k] {
			if why, isReal := realButUnsupported[k]; isReal {
				q.problems = append(q.problems, fmt.Sprintf(
					"%s: %s is not supported by the fixture filter (real furrow qualifier)", k, why))
			} else {
				q.problems = append(q.problems, fmt.Sprintf("unknown key %q (%s)", k, keyList(queryKeys)))
			}
			continue
		}
		if val == "" {
			q.problems = append(q.problems, fmt.Sprintf("%s: needs a value", k))
			continue
		}
		if k == "status" {
			k = "lane"
		}
		t.key = k
		for _, part := range strings.Split(val, ",") {
			if part == "" {
				continue
			}
			if !caseSensitiveKeys[k] {
				part = strings.ToLower(part)
			}
			t.vals = append(t.vals, part)
		}
		switch k {
		case "is":
			q.problems = refuseUnknown(t.vals, isValues, q.problems, "is")
		case "no", "has":
			q.problems = refuseUnknown(t.vals, presenceValues, q.problems, k)
		case "lane":
			for _, lv := range t.vals {
				if !v.knownLane(lv) {
					q.problems = append(q.problems, fmt.Sprintf(
						"unknown lane %q (configured: %s)", lv, strings.Join(v.lanes, ", ")))
				}
			}
		case "repo":
			// Resolve each value to a repo the board carries, the way furrow
			// resolves -r / repo: BEFORE it filters. An unresolvable one
			// leaves its raw value in place, which never gets read: a problem
			// refuses the whole query.
			for i, rv := range t.vals {
				canon, why := v.resolveRepo(rv)
				if why != "" {
					q.problems = append(q.problems, why)
					continue
				}
				t.vals[i] = canon
			}
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
		return task.Status == v
	case "id":
		return strings.HasPrefix(task.ID, v)
	case "epic":
		return task.Epic == v
	case "repo":
		return containsRepo(task.Repos, v)
	case "label":
		return containsExact(task.Labels, v)
	case "title":
		lt := strings.ToLower(task.Title)
		if t.exact {
			return lt == v
		}
		return strings.Contains(lt, v)
	case "body":
		return strings.Contains(strings.ToLower(task.Body), v)
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
		return !task.Due.IsZero() && task.Due.Before(nowFn()) && task.Closed.IsZero()
	case "stale":
		// Measured: furrow flags a DONE task too — is:stale is the update
		// window alone, not "open and forgotten".
		return !task.Updated.IsZero() && nowFn().Sub(task.Updated) > staleDays*24*time.Hour
	}
	return false
}

func hasField(t *board.Task, field string) bool {
	switch field {
	case "repo":
		return len(t.Repos) > 0
	case "label":
		return len(t.Labels) > 0
	case "deps":
		return len(t.Deps) > 0
	case "refs":
		return len(t.Refs) > 0
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
	case "closed":
		return !t.Closed.IsZero()
	case "reviewed":
		return !t.Reviewed.IsZero()
	case "due":
		return !t.Due.IsZero()
	}
	return false
}

// containsExact reports an exact, case-SENSITIVE hit: furrow's label:/epic:
// compare byte-for-byte, so `label:u` finds nothing and `label:UI` finds only
// the tasks tagged with that exact capitalisation (measured 2026-08-10).
func containsExact(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// containsRepo compares full repo names case-insensitively. The value has
// already been RESOLVED to a repo the board carries at parse time, which is
// where an unresolvable one was refused — so this never sees a short name.
func containsRepo(hay []string, needle string) bool {
	for _, h := range hay {
		if strings.EqualFold(h, needle) {
			return true
		}
	}
	return false
}

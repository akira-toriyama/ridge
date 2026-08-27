package ui

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseAddLineSplitsTokensFromTheTitle(t *testing.T) {
	tests := []struct {
		name, raw, title string
		tk               addTokens
	}{
		{name: "bare title", raw: "直すだけのタスク", title: "直すだけのタスク"},
		{name: "full set",
			raw:   `盤面から起票 value:4 effort:2 due:+1d dep:t-a check:"再現手順を書く" ref:internal/ui/addmode.go`,
			title: "盤面から起票",
			tk: addTokens{value: 4, effort: 2, due: "+1d", deps: []string{"t-a"},
				checks: []string{"再現手順を書く"}, refs: []string{"internal/ui/addmode.go"}}},
		{name: "tokens may sit anywhere",
			raw:   "value:3 タイトル語 due:2026-09-01 続き",
			title: "タイトル語 続き",
			tk:    addTokens{value: 3, due: "2026-09-01"}},
		{name: "dep comma is the -q list form",
			raw: "t dep:t-a,t-b", title: "t",
			tk: addTokens{deps: []string{"t-a", "t-b"}}},
		{name: "repeated dep/check/ref accumulate",
			raw: `t dep:t-a dep:t-b check:一つ check:"二 つ" ref:a.go ref:b.go`, title: "t",
			tk: addTokens{deps: []string{"t-a", "t-b"}, checks: []string{"一つ", "二 つ"},
				refs: []string{"a.go", "b.go"}}},
		{name: "ref keeps its own colons and a URL stays a word",
			raw: "見る ref:internal/ui/addmode.go:42 https://example.com/x",
			// A bare URL cuts at http: and "http" is not a known key.
			title: "見る https://example.com/x",
			tk:    addTokens{refs: []string{"internal/ui/addmode.go:42"}}},
		{name: "a quoted field is literal title text",
			raw: `"value:4" を直す`, title: "value:4 を直す"},
		{name: "single quotes work too",
			raw: "t check:'foo bar'", title: "t", tk: addTokens{checks: []string{"foo bar"}}},
		{name: "an unclosed quote runs to the end while typing",
			raw: `t check:"書きかけ`, title: "t", tk: addTokens{checks: []string{"書きかけ"}}},
		// Found by review: quotes were consumed ANYWHERE in a field, so an
		// apostrophe silently rewrote the title and swallowed every token
		// behind it. Mid-word quotes are literal runes now.
		{name: "an apostrophe is a literal rune, and tokens behind it still parse",
			raw: "Don't stop value:4", title: "Don't stop", tk: addTokens{value: 4}},
		{name: "mid-word quotes reach the store verbatim",
			raw: `彼は"これ"と言った`, title: `彼は"これ"と言った`},
		{name: "a quote mid-value is literal too",
			raw: `t ref:a"b`, title: "t",
			tk: addTokens{bad: []string{"ref:a\"b — `,` and `\"` cannot ride furrow's CSV flag parsing"}}},
		{name: "dep shares ref's CSV quote refusal",
			raw: `t dep:t-a"b`, title: "t",
			tk: addTokens{bad: []string{"dep:t-a\"b — `\"` cannot ride furrow's CSV flag parsing"}}},
		{name: "unknown colon words stay title",
			raw: "cifail: wait の修正", title: "cifail: wait の修正"},
		{name: "value must be a number",
			raw: "t value:高", title: "t",
			tk: addTokens{bad: []string{"value:高 — not a number"}}},
		// Found by review: value:0 parsed into the "absent" sentinel and
		// vanished without a chip, a flag, or a warning.
		{name: "out-of-range estimates are refused live, not clamped or dropped",
			raw: "t value:0 effort:9", title: "t",
			tk: addTokens{bad: []string{"value:0 — want 1..5", "effort:9 — want 1..5"}}},
		{name: "a bad due form is named live, before Enter",
			raw: "t due:someday", title: "t",
			tk: addTokens{bad: []string{"due:someday — not a date (YYYY-MM-DD / +1d …)"}}},
		{name: "empty values are named as typed, not dropped",
			raw: `t due: dep:, check:"" ref:`, title: "t",
			tk: addTokens{bad: []string{
				"due: — needs a date",
				"dep:, — needs a task id",
				`check:"" — needs text`,
				"ref: — needs a file:line or URL"}}},
		{name: "inherited keys are refused with guidance",
			raw: "t epic:e-x", title: "t",
			tk: addTokens{bad: []string{"epic:e-x — inherited from the filter; filter first, or quote it to keep it in the title"}}},
		// status:/lane: are NOT the filter's — the add lands in the focused
		// column — so their guidance must not say "filter first" (review).
		{name: "status and lane name the focused column instead",
			raw: "t lane:ready", title: "t",
			tk: addTokens{bad: []string{"lane:ready — the add lands in the focused column; focus it first, or quote it to keep it in the title"}}},
		{name: "last value/effort/due wins",
			raw: "t value:1 value:5 due:+1d due:+2d", title: "t",
			tk: addTokens{value: 5, due: "+2d"}},
		{name: "is:draft marks the add a draft (t-v4pp)",
			raw: "t is:draft", title: "t", tk: addTokens{draft: true}},
		{name: "the value folds case, like -q's own match",
			raw: "t is:Draft", title: "t", tk: addTokens{draft: true}},
		{name: "every other is: value is refused with guidance",
			raw: "t is:blocked", title: "t",
			tk: addTokens{bad: []string{"is:blocked — only is:draft applies to an add; filter the view instead, or quote it to keep it in the title"}}},
		{name: "a quoted is:draft stays title text",
			raw: `"is:draft" の説明`, title: "is:draft の説明"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			title, tk := parseAddLine(tc.raw)
			if title != tc.title {
				t.Errorf("title = %q, want %q", title, tc.title)
			}
			if !reflect.DeepEqual(tk, tc.tk) {
				t.Errorf("tokens = %+v, want %+v", tk, tc.tk)
			}
		})
	}
}

func TestQuickAddInlineTokensLandOnTheTask(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.curLane = m.b.LaneIndex("ready")
	press(m, "a")
	m.add.input.SetValue(`拡張つき起票 value:4 effort:2 due:+1d dep:t-jv3j check:"再現手順を書く" ref:internal/ui/addmode.go`)
	commitAdd(t, m)

	cur := m.curTask()
	if cur == nil || cur.Title != "拡張つき起票" {
		t.Fatalf("selection = %+v, want the new task with the tokens parsed OUT of the title", cur)
	}
	if cur.Value != 4 || cur.Effort != 2 {
		t.Errorf("value/effort = %d/%d, want 4/2", cur.Value, cur.Effort)
	}
	if cur.Due.IsZero() {
		t.Error("due:+1d did not land")
	}
	if len(cur.Deps) != 1 || cur.Deps[0] != "t-jv3j" {
		t.Errorf("deps = %v, want [t-jv3j]", cur.Deps)
	}
	if len(cur.Checklist) != 1 || cur.Checklist[0].Text != "再現手順を書く" || cur.Checklist[0].Done {
		t.Errorf("checklist = %+v, want one unchecked 再現手順を書く", cur.Checklist)
	}
	if len(cur.Refs) != 1 || cur.Refs[0] != "internal/ui/addmode.go" {
		t.Errorf("refs = %v", cur.Refs)
	}
}

func TestQuickAddRefusesBadTokensAndKeepsTheLine(t *testing.T) {
	m := boardModel(t, 240, 50)
	before := len(m.b.Tasks())

	// A non-numeric estimate is refused in-modal, line intact.
	press(m, "a")
	const raw = "t value:高"
	m.add.input.SetValue(raw)
	commitAdd(t, m)
	if m.mode != modeAdd || m.add.input.Value() != raw {
		t.Fatal("a bad token must keep the modal open with the typed line intact")
	}
	if !strings.Contains(m.status, "not a number") {
		t.Errorf("status = %q, want it to name the offender", m.status)
	}

	// A tokens-only line is a TOKEN problem: the refusal must name the bad
	// token, not "a title cannot be empty" (review found the order flipped).
	m.add.input.SetValue("value:高")
	commitAdd(t, m)
	if !strings.Contains(m.status, "not a number") {
		t.Errorf("status = %q, want the token named, not the empty title", m.status)
	}

	// Range, due grammar and the ref CSV caveat refuse before the store
	// round trip.
	for _, bad := range []string{"t value:9", "t due:someday", `t ref:'a,b'`} {
		m.add.input.SetValue(bad)
		commitAdd(t, m)
		if m.mode != modeAdd {
			t.Fatalf("%q must be refused in-modal", bad)
		}
	}
	if len(m.b.Tasks()) != before {
		t.Error("refused adds must create nothing")
	}

	// An unknown dep is the store's refusal (mirrored by the fixture): the
	// modal reopens carrying the RAW line, tokens and all.
	m.add.input.SetValue("t dep:t-nope")
	commitAdd(t, m)
	if m.mode != modeAdd || m.add == nil || m.add.input.Value() != "t dep:t-nope" {
		t.Fatalf("a store-refused add must reopen with the raw line; mode=%v", m.mode)
	}
	press(m, "esc")
}

// The draft trio (t-v4pp): the token creates a repo-less task, the filter
// inherits the draft, and the repo+draft clash is refused in-modal.
func TestQuickAddCreatesADraft(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.curLane = m.b.LaneIndex("backlog")
	press(m, "a")
	m.add.input.SetValue("思いつきの控え is:draft")
	commitAdd(t, m)

	cur := m.curTask()
	if cur == nil || cur.Title != "思いつきの控え" {
		t.Fatalf("selection = %+v, want the new draft with the token parsed out", cur)
	}
	if len(cur.Repos) != 0 {
		t.Errorf("repos = %v, want none — a draft attaches no repo", cur.Repos)
	}
}

func TestQuickAddInheritsDraftFromTheFilter(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.applyFilter("is:draft")
	press(m, "a")
	if m.add == nil || !m.add.opts.Draft {
		t.Fatal("adding under is:draft must inherit the draft — a repo-attached add would vanish from the very view it was added into")
	}
	if out := frame(m); !strings.Contains(out, "draft (no repo)") {
		t.Error("the chips row must declare the draft, never stamp it silently")
	}
	m.add.input.SetValue("控えを一枚")
	commitAdd(t, m)
	cur := m.curTask()
	if cur == nil || cur.Title != "控えを一枚" || len(cur.Repos) != 0 {
		t.Fatalf("task = %+v, want a repo-less draft selected under the still-matching filter", cur)
	}
}

// A refused draft must come back CLEARABLE: the reopened modal carries the
// inherited context only, so the typed is:draft lives in the line alone and
// deleting it really clears it. The first cut stored the composed opts, whose
// OR'd Draft no edit could undo (found by review).
func TestReopenedRefusalDoesNotPinTheTypedDraft(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.curLane = m.b.LaneIndex("backlog")
	press(m, "a")
	m.add.input.SetValue("控え is:draft dep:t-nope") // the unknown dep is the store's refusal
	commitAdd(t, m)
	if m.mode != modeAdd || m.add == nil {
		t.Fatal("the refusal must reopen the modal")
	}
	if m.add.opts.Draft {
		t.Fatal("the reopened opts must carry the inherited context only — a typed is:draft pinned here cannot be cleared")
	}
	m.add.input.SetValue("控え") // the user deletes both tokens
	commitAdd(t, m)
	cur := m.curTask()
	if cur == nil || cur.Title != "控え" {
		t.Fatalf("selection = %+v, want the re-committed task", cur)
	}
	if len(cur.Repos) == 0 {
		t.Error("the deleted is:draft still stuck — the task landed as a draft")
	}
}

func TestQuickAddRefusesDraftWithAnInheritedRepo(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.applyFilter("repo:tomo/kyushu-trip")
	press(m, "a")
	const raw = "t is:draft"
	m.add.input.SetValue(raw)
	// The clash is on the ⚠ row BEFORE Enter, like every other live refusal.
	if out := frame(m); !strings.Contains(out, "draft conflicts with repo") {
		t.Error("the modal must name the repo/draft clash before Enter does")
	}
	commitAdd(t, m)
	if m.mode != modeAdd || m.add.input.Value() != raw {
		t.Fatal("the clash must be refused in-modal with the typed line intact")
	}
	if !strings.Contains(m.status, "conflicts with repo") {
		t.Errorf("status = %q, want the clash named", m.status)
	}
	press(m, "esc")
}

func TestQuickAddModalEchoesTokensAsChips(t *testing.T) {
	m := boardModel(t, 240, 50)
	if err := m.demoState("add"); err != nil {
		t.Fatal(err)
	}
	out := frame(m)
	for _, want := range []string{"value 4", "due +1d", "dep t-jv3j", "check 再現",
		"effort:高 — not a number"} {
		if !strings.Contains(out, want) {
			t.Errorf("the modal frame is missing %q — the live echo must show what will be stamped", want)
		}
	}
}

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
		{name: "unknown colon words stay title",
			raw: "cifail: wait の修正", title: "cifail: wait の修正"},
		{name: "value must be a number",
			raw: "t value:高", title: "t",
			tk: addTokens{bad: []string{"value:高 — not a number"}}},
		{name: "empty values are named, not dropped",
			raw: "t due: dep:, check: ref:", title: "t",
			tk: addTokens{bad: []string{
				"due: — needs a date",
				"dep:, — needs a task id",
				"check: — needs text",
				"ref: — needs a file:line or URL"}}},
		{name: "inherited keys are refused with guidance",
			raw: "t epic:e-x", title: "t",
			tk: addTokens{bad: []string{"epic:e-x — inherited from the filter; filter first, or quote it to keep it in the title"}}},
		{name: "last value/effort/due wins",
			raw: "t value:1 value:5 due:+1d due:+2d", title: "t",
			tk: addTokens{value: 5, due: "+2d"}},
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

	// Syntax: a non-numeric estimate is refused in-modal, line intact.
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

	// Semantics: board-side validation (estimate range, due grammar, ref CSV)
	// refuses before the store round trip.
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

func TestQuickAddModalEchoesTokensAsChips(t *testing.T) {
	m := boardModel(t, 240, 50)
	if err := m.demoState("add"); err != nil {
		t.Fatal(err)
	}
	out := frame(m)
	for _, want := range []string{"value 4", "due +1d", "dep t-jv3j", "check 再現手順を書く",
		"effort:高 — not a number"} {
		if !strings.Contains(out, want) {
			t.Errorf("the modal frame is missing %q — the live echo must show what will be stamped", want)
		}
	}
}

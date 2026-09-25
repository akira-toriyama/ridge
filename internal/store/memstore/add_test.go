package memstore

import (
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The auto-attach mirror (fixtureDefaultRepo) — the three rows measured
// against the real binary's `default_repo` rule. Without the mirror a plain
// mock add landed repo-less, i.e. as a draft the card then labeled as one
// (found by review), so the plain row is the load-bearing assertion.
func TestAddMirrorsTheDefaultRepoAutoAttach(t *testing.T) {
	p := New()

	plain, err := p.Add("素の起票", board.AddOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Board().Task(plain); len(got.Repos) != 1 || got.Repos[0] != fixtureDefaultRepo {
		t.Errorf("plain add repos = %v, want the board's %s auto-attached", got.Repos, fixtureDefaultRepo)
	}

	explicit, err := p.Add("repo 指定の起票", board.AddOptions{Repo: "tomo/joubisai"})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Board().Task(explicit); len(got.Repos) != 1 || got.Repos[0] != "tomo/joubisai" {
		t.Errorf("explicit repos = %v, want the inherited repo to win over the default", got.Repos)
	}

	draft, err := p.Add("draft の起票", board.AddOptions{Draft: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Board().Task(draft); len(got.Repos) != 0 {
		t.Errorf("draft repos = %v, want none — --draft suppresses the auto-attach", got.Repos)
	}
}

// A quick add with a rule (t-zbmv): the fixture stores the spelling as typed
// (it compiles none) anchored at the first due, as furrow does; a rule with
// no due is Validate's refusal, before any task exists.
func TestAddCarriesTheRuleAnchoredAtTheDue(t *testing.T) {
	p := New()
	id, err := p.Add("週次の締め", board.AddOptions{Due: "2026-10-02", Repeat: "weekly"})
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Board().Task(id); got.Repeat != "weekly" || got.Due.IsZero() || !got.RepeatAnchor.Equal(got.Due) {
		t.Errorf("task = %+v, want repeat weekly anchored at the due", got)
	}
	before := len(p.Board().Tasks())
	if _, err := p.Add("due なし", board.AddOptions{Repeat: "weekly"}); err == nil || !strings.Contains(err.Error(), "needs a due") {
		t.Errorf("a rule with no due = %v, want the refusal", err)
	}
	if len(p.Board().Tasks()) != before {
		t.Error("a refused add must create nothing")
	}
}

package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// The demo subjects are chosen by shape (demo.go's demo* selectors), so on
// the fixture every selector must land on the row its frame was written for
// — otherwise a fixture edit moves a demo onto another row and every fit
// test stays green while the frame quietly changes subject.
func TestDemoSubjectsAreTheFixtureRowsTheyWereWrittenFor(t *testing.T) {
	m := boardModel(t, 240, 50)
	task := func(f func(string) (*board.Task, error)) func() (string, error) {
		return func() (string, error) {
			x, err := f("test")
			if err != nil {
				return "", err
			}
			return x.ID, nil
		}
	}
	box := func(f func(string) (board.EpicInfo, error)) func() (string, error) {
		return func() (string, error) {
			x, err := f("test")
			if err != nil {
				return "", err
			}
			return x.ID, nil
		}
	}
	str := func(f func(string) (string, error)) func() (string, error) {
		return func() (string, error) { return f("test") }
	}
	for _, tc := range []struct {
		selector string
		want     string
		got      func() (string, error)
	}{
		{"demoAnyTask", "t-jv3j", task(m.demoAnyTask)},
		{"demoEditTask", "t-9sa6", task(m.demoEditTask)},
		{"demoRefsTask", "t-9sa6", task(m.demoRefsTask)},
		{"demoMixedDepsTask", "t-jv3j", task(m.demoMixedDepsTask)},
		{"demoMostDepsTask", "t-t38k", task(m.demoMostDepsTask)},
		{"demoRootTask", "t-ehk7", task(m.demoRootTask)},
		{"demoEpicDepsTask", "t-y4st", task(m.demoEpicDepsTask)},
		{"demoRichBox", "e-c4mt", box(m.demoRichBox)},
		{"demoActiveBox", "e-fw2m", box(m.demoActiveBox)},
		{"demoClosedBox", "e-2b7h", box(m.demoClosedBox)},
		{"demoLabel", "bbq", str(m.demoLabel)},
		{"demoRepo", "tomo/kyushu-trip", str(m.demoRepo)},
	} {
		got, err := tc.got()
		if err != nil {
			t.Errorf("%s: %v", tc.selector, err)
			continue
		}
		if got != tc.want {
			t.Errorf("%s picked %s on the fixture, want %s", tc.selector, got, tc.want)
		}
	}
	// epiclist's frame shows all three resolutions, which demoRichBox does not
	// ask for (a dangling epic dep is a furrow lint ERROR): pin that the box
	// it lands on is the one carrying them.
	rich, err := m.demoRichBox("test")
	if err != nil {
		t.Fatal(err)
	}
	var open, closed, missing bool
	for _, d := range rich.Deps {
		switch de := m.b.Epic(d); {
		case de == nil:
			missing = true
		case de.Closed.IsZero():
			open = true
		default:
			closed = true
		}
	}
	if !open || !closed || !missing {
		t.Errorf("%s must wait on an open box, a closed box and an unresolvable id (open=%t closed=%t missing=%t)",
			rich.ID, open, closed, missing)
	}
}

// Every -demo must be producible on a board that is NOT the fixture. The
// board here is the fixture with every id renamed, so any demo that still
// names a fixture id fails here and nowhere else; the shapes the predicates
// ask for are all still present, so a refusal is a hardcoded id, not a
// missing shape. Labels, repos, lanes and titles stay the fixture's — a
// literal from those vocabularies is caught by the bare-board refusals
// below, not here.
func TestEveryDemoIsProducibleWithoutTheFixtureIDs(t *testing.T) {
	b := renamedFixture(t)
	for _, d := range DemoNames {
		m := New(memstore.NewWith(b), Options{})
		out, err := m.Dump(240, 50, d, true)
		if err != nil {
			t.Errorf("-demo %s over a board without the fixture's ids: %v", d, err)
			continue
		}
		for _, id := range []string{"t-9sa6", "t-jv3j", "t-t38k", "t-ehk7", "t-y4st", "e-c4mt", "e-fw2m", "e-2b7h"} {
			if strings.Contains(out, id) {
				t.Errorf("-demo %s printed the fixture id %s on a board that holds no such id", d, id)
			}
		}
	}
}

// A selector's refusal must say what shape it needed, on a board that has
// none of it, so the next person adding a demo over other data learns the
// precondition from the error and not from reading demo.go. advSmallBoard
// carries no labels, repos, refs, checklists, deps or boxes, so any of those
// vocabularies hardcoded in a demo would draw here instead of refusing.
func TestDemoRefusalNamesTheMissingShape(t *testing.T) {
	m := advSmallModel(t, 240, 50)
	for _, tc := range []struct{ demo, want string }{
		{"edit", "checklist"},
		{"editrepeat", "repeat rule"},
		{"refs", "two or more refs"},
		{"editdeps", "waits on both"},
		{"epicdeps", "filed under a box"},
		{"epic", "inactive box"},
		{"epicconfirm", "is active"},
		{"epicdoneparked", "parked in a terminal lane"},
		{"epicshut", "is closed"},
		{"boxesall", "is closed"},
		{"slice", "carries a label"},
		{"epicnew", "carries a repo"},
		{"repeat", "repeat rule"},
		{"repeatdone", "repeat rule"},
		{"done", "frees exactly one"},
	} {
		_, err := m.Dump(240, 50, tc.demo, true)
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("-demo %s on a bare board: err = %v, want one naming %q", tc.demo, err, tc.want)
		}
	}
}

// renamedFixture is the fixture with every task and box id rewritten
// (t-q001…, e-q001…), edges and the bodies' [[wikilinks]] included; the one
// dangling epic dep stays dangling. Same lengths as the originals, so no
// layout differs.
func renamedFixture(t *testing.T) *board.Board {
	t.Helper()
	src := memstore.New().Board()
	rename := map[string]string{}
	for i, task := range src.Tasks() {
		rename[task.ID] = fmt.Sprintf("t-q%03d", i+1)
	}
	for i, e := range src.EpicsAll() {
		rename[e.ID] = fmt.Sprintf("e-q%03d", i+1)
	}
	ren := func(id string) string {
		if to, ok := rename[id]; ok {
			return to
		}
		return id
	}
	renAll := func(ids []string) []string {
		if ids == nil {
			return nil
		}
		out := make([]string, len(ids))
		for i, id := range ids {
			out[i] = ren(id)
		}
		return out
	}
	tasks := make([]*board.Task, 0, len(src.Tasks()))
	for _, task := range src.Tasks() {
		c := *task
		c.ID, c.Epic, c.Deps = ren(c.ID), ren(c.Epic), renAll(c.Deps)
		for from, to := range rename {
			c.Body = strings.ReplaceAll(c.Body, from, to)
		}
		tasks = append(tasks, &c)
	}
	epics := make([]board.EpicInfo, 0, len(src.EpicsAll()))
	for _, e := range src.EpicsAll() {
		e.ID, e.Deps, e.OpenDeps = ren(e.ID), renAll(e.Deps), renAll(e.OpenDeps)
		epics = append(epics, e)
	}
	b := board.NewBoard(tasks, epics...)
	if b.Task("t-jv3j") != nil || b.Epic("e-fw2m") != nil {
		t.Fatal("the rename did not take")
	}
	return b
}

// Each selector's clauses are the frame's preconditions, so a near miss —
// the shape with one clause short — must be REFUSED, not picked: a one-item
// checklist parks edit's cursor past the list, one ref draws the refs editor
// half-empty, a box with a free repo slot has no precondition line to show.
// Without these a clause can be deleted and every fixture pin stays green,
// because the fixture never holds the near miss (measured: three such
// deletions survived the pins above).
func TestDemoSelectorsRefuseTheNearMiss(t *testing.T) {
	one := []board.ChecklistItem{{Text: "one"}}
	two := []board.ChecklistItem{{Text: "one"}, {Text: "two"}}
	openBox := board.EpicInfo{ID: "e-open", Title: "open", Repos: []string{"r/a"}}
	held := board.EpicInfo{ID: "e-held", Title: "held", Active: true, Repos: []string{"r/a"}}
	task := func(f func(string) (*board.Task, error)) func(*Model) (string, error) {
		return func(*Model) (string, error) { x, err := f("t"); return idOf(x), err }
	}
	for _, tc := range []struct {
		name  string
		tasks []*board.Task
		epics []board.EpicInfo
		pick  func(*Model) (string, error)
	}{
		{"edit: a one-item checklist with a label", []*board.Task{
			{ID: "t-a", Title: "a", Status: "backlog", Priority: 1, Labels: []string{"l"}, Checklist: one},
		}, nil, func(m *Model) (string, error) { return task(m.demoEditTask)(m) }},
		{"edit: a two-item checklist with no label", []*board.Task{
			{ID: "t-a", Title: "a", Status: "backlog", Priority: 1, Checklist: two},
		}, nil, func(m *Model) (string, error) { return task(m.demoEditTask)(m) }},
		{"refs: one ref", []*board.Task{
			{ID: "t-a", Title: "a", Status: "backlog", Priority: 1, Refs: []string{"https://x"}},
		}, nil, func(m *Model) (string, error) { return task(m.demoRefsTask)(m) }},
		{"mixed deps: both deps open", []*board.Task{
			{ID: "t-a", Title: "a", Status: "backlog", Priority: 1, Deps: []string{"t-b", "t-c"}},
			{ID: "t-b", Title: "b", Status: "backlog", Priority: 2},
			{ID: "t-c", Title: "c", Status: "backlog", Priority: 3},
		}, nil, func(m *Model) (string, error) { return task(m.demoMixedDepsTask)(m) }},
		{"mixed deps: both deps done", []*board.Task{
			{ID: "t-a", Title: "a", Status: "backlog", Priority: 1, Deps: []string{"t-b", "t-c"}},
			{ID: "t-b", Title: "b", Status: "done", Priority: 2},
			{ID: "t-c", Title: "c", Status: "done", Priority: 3},
		}, nil, func(m *Model) (string, error) { return task(m.demoMixedDepsTask)(m) }},
		{"root: the blocker waits on something itself", []*board.Task{
			{ID: "t-a", Title: "a", Status: "backlog", Priority: 1, Deps: []string{"t-b"}},
			{ID: "t-b", Title: "b", Status: "backlog", Priority: 2, Deps: []string{"t-c"}},
			{ID: "t-c", Title: "c", Status: "done", Priority: 3},
		}, nil, func(m *Model) (string, error) { return task(m.demoRootTask)(m) }},
		{"epic deps: the box's every dep is still open", []*board.Task{
			{ID: "t-a", Title: "a", Status: "backlog", Priority: 1, Epic: "e-w"},
		}, []board.EpicInfo{openBox, {ID: "e-w", Title: "w", Deps: []string{"e-open"}, OpenDeps: []string{"e-open"}}},
			func(m *Model) (string, error) { return task(m.demoEpicDepsTask)(m) }},
		{"rich box: its repo slot is free", []*board.Task{
			{ID: "t-a", Title: "a", Status: "backlog", Priority: 1, Repos: []string{"r/a", "r/b"}},
		}, []board.EpicInfo{openBox, {ID: "e-x", Title: "x", Goal: "g", Repos: []string{"r/b"}, Deps: []string{"e-open"}, OpenDeps: []string{"e-open"}}},
			func(m *Model) (string, error) { x, err := m.demoRichBox("t"); return x.ID, err }},
		{"rich box: no goal", []*board.Task{
			{ID: "t-a", Title: "a", Status: "backlog", Priority: 1, Repos: []string{"r/a"}},
		}, []board.EpicInfo{openBox, held, {ID: "e-x", Title: "x", Repos: []string{"r/a"}, Deps: []string{"e-open"}, OpenDeps: []string{"e-open"}}},
			func(m *Model) (string, error) { x, err := m.demoRichBox("t"); return x.ID, err }},
		{"rich box: it is the active one", []*board.Task{
			{ID: "t-a", Title: "a", Status: "backlog", Priority: 1, Repos: []string{"r/a"}},
		}, []board.EpicInfo{openBox, {ID: "e-x", Title: "x", Goal: "g", Active: true, Repos: []string{"r/a"}, Deps: []string{"e-open"}, OpenDeps: []string{"e-open"}}},
			func(m *Model) (string, error) { x, err := m.demoRichBox("t"); return x.ID, err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := New(memstore.NewWith(board.NewBoard(tc.tasks, tc.epics...)), Options{})
			got, err := tc.pick(m)
			if err == nil {
				t.Errorf("picked %s; the near miss must be refused", got)
			}
		})
	}
	// And the shape itself, one clause over the near miss, IS picked — so the
	// refusals above are the clauses' doing, not a selector that refuses
	// everything.
	m := New(memstore.NewWith(board.NewBoard([]*board.Task{
		{ID: "t-a", Title: "a", Status: "backlog", Priority: 1, Repos: []string{"r/a"}, Labels: []string{"l"}, Checklist: two, Refs: []string{"a.go:1", "https://x"}},
	}, openBox, held, board.EpicInfo{ID: "e-x", Title: "x", Goal: "g", Repos: []string{"r/a"}, Deps: []string{"e-open"}, OpenDeps: []string{"e-open"}})), Options{})
	if x, err := m.demoEditTask("t"); err != nil || x.ID != "t-a" {
		t.Errorf("demoEditTask on the full shape: %v, %v", idOf(x), err)
	}
	if x, err := m.demoRefsTask("t"); err != nil || x.ID != "t-a" {
		t.Errorf("demoRefsTask on the full shape: %v, %v", idOf(x), err)
	}
	if x, err := m.demoRichBox("t"); err != nil || x.ID != "e-x" {
		t.Errorf("demoRichBox on the full shape: %v, %v", x.ID, err)
	}
}

func idOf(t *board.Task) string {
	if t == nil {
		return ""
	}
	return t.ID
}

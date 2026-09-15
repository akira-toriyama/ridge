package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
)

// The demo subjects are chosen by shape (dump.go's demo* selectors), so on
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
// missing shape.
func TestEveryDemoIsProducibleOffTheFixture(t *testing.T) {
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
// precondition from the error and not from reading dump.go.
func TestDemoRefusalNamesTheMissingShape(t *testing.T) {
	m := advSmallModel(t, 240, 50)
	for _, tc := range []struct{ demo, want string }{
		{"edit", "checklist"},
		{"refs", "refs"},
		{"editdeps", "waits on both"},
		{"epicdeps", "filed under a box"},
		{"epic", "inactive box"},
		{"epicconfirm", "is active"},
		{"epicshut", "is closed"},
		{"boxesall", "is closed"},
		{"slice", "carries a label"},
		{"epicnew", "carries a repo"},
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

package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// rmModel opens the edit overlay's delete row on id: the gate, with the
// fixture's synchronous preview landed.
func rmModel(t *testing.T, id string) *Model {
	t.Helper()
	m := editModel(t, id)
	m.edit.menuIdx = int(fieldDelete)
	press(m, "enter")
	if m.edit == nil || m.edit.stage != stageGate || m.edit.field != fieldDelete {
		t.Fatalf("the delete row did not open its gate: %+v", m.edit)
	}
	if m.edit.rm.report == nil {
		t.Fatalf("the fixture's preview is synchronous and must have landed: err=%q", m.edit.rm.err)
	}
	return m
}

// An unreferenced task: the gate says nothing points at it and ⏎ deletes —
// store-first, so the overlay closes, the card stays until the write lands,
// and the re-read drops it.
func TestDeleteRowDeletesAnUnreferencedTask(t *testing.T) {
	m := rmModel(t, "t-7wdg")
	out := frame(m)
	for _, want := range []string{"delete this task", "t-7wdg", "nothing points at it", "⏎ deletes · esc backs out"} {
		if !strings.Contains(out, want) {
			t.Errorf("the gate lost %q:\n%s", want, out)
		}
	}
	if !strings.Contains(m.status, "rm t-7wdg — nothing points at it · ⏎ deletes") {
		t.Errorf("status = %q", m.status)
	}
	press(m, "enter")
	if m.edit != nil {
		t.Error("the ⏎ that writes must close the overlay")
	}
	if m.b.Task("t-7wdg") == nil {
		t.Error("store-first: the card stays until the write lands")
	}
	if !strings.Contains(m.status, "rm t-7wdg — waiting for furrow") {
		t.Errorf("status = %q, want the wait", m.status)
	}
	drainPersists(m, t)
	if m.b.Task("t-7wdg") != nil {
		t.Error("the re-read must drop the removed task")
	}
	if !strings.HasPrefix(m.status, "rm t-7wdg") {
		t.Errorf("landing note = %q, want the label", m.status)
	}
}

// A referenced task: the first ⏎ only arms --force, with the list of what it
// will sever on screen; the second severs and deletes, and the landing note
// says how many references went.
func TestDeleteRowArmsForceOnAReferencedTask(t *testing.T) {
	m := rmModel(t, "t-t38k")
	n := m.edit.rm.report.References.Count()
	if n == 0 {
		t.Fatal("setup: t-t38k is the fixture's most-referenced task")
	}
	out := frame(m)
	for _, want := range []string{"still referenced — ", "dep edge(s) from t-jv3j", "dep      t-jv3j → t-t38k", "⏎ arms --force"} {
		if !strings.Contains(out, want) {
			t.Errorf("the gate lost %q:\n%s", want, out)
		}
	}
	press(m, "enter")
	if m.edit == nil || !m.edit.rm.armed {
		t.Fatal("the first ⏎ on a referenced target must only arm --force")
	}
	if len(m.pending) != 0 || m.b.Task("t-t38k") == nil {
		t.Fatal("arming writes nothing")
	}
	if want := fmt.Sprintf("⏎ severs %d reference(s) and deletes", n); !strings.Contains(frame(m), want) || !strings.Contains(m.status, "--force armed") {
		t.Errorf("the armed gate must say what the next ⏎ does: status %q", m.status)
	}
	press(m, "enter")
	if m.edit != nil {
		t.Error("the ⏎ that writes must close the overlay")
	}
	drainPersists(m, t)
	if m.b.Task("t-t38k") != nil {
		t.Error("the re-read must drop the removed task")
	}
	if jv := m.b.Task("t-jv3j"); jv == nil || slices.Contains(jv.Deps, "t-t38k") {
		t.Errorf("--force must have dropped t-jv3j's edge onto the removed task: %+v", jv)
	}
	if want := fmt.Sprintf("severed %d reference(s)", n); !strings.Contains(m.status, want) {
		t.Errorf("the landing note must say what --force severed: %q", m.status)
	}
}

// esc backs out, armed or not, writing nothing — and reopening the row starts
// disarmed with a fresh preview.
func TestEscBacksOutOfTheDeleteGateDisarmed(t *testing.T) {
	m := rmModel(t, "t-t38k")
	press(m, "enter") // arm
	press(m, "esc")
	if m.edit == nil || m.edit.stage != stageMenu {
		t.Fatal("esc must return to the menu")
	}
	if len(m.pending) != 0 || m.b.Task("t-t38k") == nil {
		t.Error("backing out writes nothing")
	}
	press(m, "enter")
	if m.edit.stage != stageGate || m.edit.rm.armed {
		t.Error("reopening the row must start disarmed")
	}
}

// The box's delete row: members are references, so the gate arms; --force
// unfiles them and the box leaves the board.
func TestEpicDeleteRowUnfilesMembersUnderForce(t *testing.T) {
	m := boardModel(t, 240, 50)
	m.toggleSlice()
	m.sliceField = sliceEpic
	m.enterEpic("e-c4mt")
	m.epic.menuIdx = int(epicFieldDelete)
	press(m, "enter")
	if m.epic == nil || m.epic.stage != stageGate || m.epic.field != epicFieldDelete || m.epic.rm.report == nil {
		t.Fatalf("the box's delete row did not open its gate: %+v", m.epic)
	}
	out := frame(m)
	for _, want := range []string{"delete this box", "e-c4mt", "member   t-y4st in e-c4mt (unfiled)", "⏎ arms --force"} {
		if !strings.Contains(out, want) {
			t.Errorf("the gate lost %q:\n%s", want, out)
		}
	}
	press(m, "enter") // arm
	press(m, "enter") // write
	if m.epic != nil {
		t.Error("the ⏎ that writes must close the overlay")
	}
	drainPersists(m, t)
	if m.b.Epic("e-c4mt") != nil {
		t.Error("the re-read must drop the removed box")
	}
	if y := m.b.Task("t-y4st"); y == nil || y.Epic != "" {
		t.Errorf("t-y4st must be unfiled: %+v", y)
	}
}

// The four frames carry what they exist to show (footer_test proves only
// that each differs from the bare board).
func TestDeleteDemoFramesCarryWhatTheyExistFor(t *testing.T) {
	for _, tc := range []struct {
		demo string
		want []string
	}{
		{"rm", []string{"delete this task", "nothing points at it", "⏎ deletes"}},
		{"rmreferenced", []string{"delete this task", "still referenced —", "dep      ", "link     ", "⏎ arms --force"}},
		{"rmforce", []string{"--force armed", "reference(s) and deletes"}},
		{"epicrm", []string{"delete this box", "member(s):", "member   ", "⏎ arms --force"}},
	} {
		t.Run(tc.demo, func(t *testing.T) {
			out := strings.Join(dumpFrame(t, 240, 44, tc.demo), "\n")
			for _, want := range tc.want {
				if !strings.Contains(out, want) {
					t.Errorf("-demo %s lost %q", tc.demo, want)
				}
			}
		})
	}
}

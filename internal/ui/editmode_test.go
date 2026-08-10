package ui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

func press(m *Model, keys ...string) {
	for _, k := range keys {
		m.Update(keyMsg(k))
	}
}

func keyMsg(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "up":
		return tea.KeyPressMsg{Code: tea.KeyUp}
	case "down":
		return tea.KeyPressMsg{Code: tea.KeyDown}
	}
	return tea.KeyPressMsg{Code: rune(k[0]), Text: k}
}

// drainPersists runs every queued write to completion, the way the program
// loop would, so a test can assert on the store-visible outcome.
func drainPersists(m *Model, t *testing.T) {
	t.Helper()
	for i := 0; i < 32 && (m.inflight || len(m.pending) > 0); i++ {
		cmd := m.firePersist()
		if cmd == nil {
			break
		}
		m.Update(cmd())
	}
	if m.inflight || len(m.pending) > 0 {
		t.Fatal("the persist queue did not drain")
	}
}

func editModel(t *testing.T, id string) *Model {
	t.Helper()
	m := boardModel(t, 240, 50)
	if !m.selectID(id, false) {
		t.Fatalf("could not select %s", id)
	}
	m.enterEdit()
	if m.mode != modeEdit || m.edit == nil {
		t.Fatal("enterEdit did not open the overlay")
	}
	return m
}

func TestEnterEditsWithPeekAndMovesWithout(t *testing.T) {
	// Board, no peek: Enter is move mode — the muscle memory stays.
	m := boardModel(t, 240, 50)
	press(m, "enter")
	if m.mode != modeMove {
		t.Errorf("board Enter without a peek must lift the card, mode=%d", m.mode)
	}
	press(m, "esc")

	// Peek open: Enter edits.
	m.peekOpen = true
	press(m, "enter")
	if m.mode != modeEdit {
		t.Errorf("board Enter with the peek open must edit, mode=%d", m.mode)
	}
	press(m, "esc")

	// Table rows: Enter edits (GitHub's Enter = cell editing).
	m2 := boardModel(t, 240, 50)
	m2.view = viewTable
	press(m2, "enter")
	if m2.mode != modeEdit {
		t.Errorf("table Enter must edit, mode=%d", m2.mode)
	}
}

func TestEditValuePickAppliesAndPersists(t *testing.T) {
	m := editModel(t, "t-9sa6")
	m.edit.menuIdx = int(fieldValue)
	press(m, "enter") // open the picker
	if m.edit.stage != stagePick {
		t.Fatalf("stage = %d, want pick", m.edit.stage)
	}
	press(m, "3")
	if got := m.b.Task("t-9sa6").Value; got != 3 {
		t.Errorf("value = %d, want 3 (optimistic apply)", got)
	}
	if m.edit.stage != stageMenu {
		t.Error("a pick must return to the menu")
	}
	drainPersists(m, t)

	// 0 clears.
	m.edit.menuIdx = int(fieldValue)
	press(m, "enter", "0")
	if got := m.b.Task("t-9sa6").Value; got != 0 {
		t.Errorf("value = %d, want cleared", got)
	}
	drainPersists(m, t)
}

func TestEditLabelToggleRoundTrips(t *testing.T) {
	m := editModel(t, "t-9sa6") // labels: [ui]
	m.edit.menuIdx = int(fieldLabels)
	press(m, "enter")
	rows := m.editListRows(m.b.Task("t-9sa6"))
	idx := -1
	for i, r := range rows {
		if r == "ui" {
			idx = i
		}
	}
	if idx < 0 {
		t.Fatalf("label vocabulary %v lacks ui", rows)
	}
	m.edit.listIdx = idx
	press(m, "x")
	if containsStrUI(m.b.Task("t-9sa6").Labels, "ui") {
		t.Error("toggling an owned label must remove it")
	}
	press(m, "x")
	if !containsStrUI(m.b.Task("t-9sa6").Labels, "ui") {
		t.Error("toggling again must add it back")
	}
	drainPersists(m, t)
}

func TestEditEpicFileAndUnfile(t *testing.T) {
	m := editModel(t, "t-ehk7") // unfiled in the fixture
	m.edit.menuIdx = int(fieldEpic)
	press(m, "enter")
	m.edit.listIdx = 1 // the one fixture epic
	press(m, "enter")
	if got := m.b.Task("t-ehk7").Epic; got != "e-fw2m" {
		t.Errorf("epic = %q, want e-fw2m", got)
	}
	drainPersists(m, t)

	m.edit.menuIdx = int(fieldEpic)
	press(m, "enter") // reopen; row 0 = (unfiled)
	press(m, "enter")
	if got := m.b.Task("t-ehk7").Epic; got != "" {
		t.Errorf("epic = %q, want unfiled", got)
	}
	drainPersists(m, t)
}

func TestEditDueRefusesGarbageAndAcceptsForms(t *testing.T) {
	m := editModel(t, "t-9sa6")
	m.edit.menuIdx = int(fieldDue)
	press(m, "enter")
	if m.edit.stage != stageInput {
		t.Fatal("due must open the text input")
	}
	m.edit.input.SetValue("someday")
	press(m, "enter")
	if !m.b.Task("t-9sa6").Due.IsZero() {
		t.Error("garbage due must refuse, not guess")
	}
	if !m.statusErr {
		t.Error("the refusal must be surfaced in the status line")
	}

	m.edit.menuIdx = int(fieldDue)
	press(m, "enter")
	m.edit.input.SetValue("2026-09-01")
	press(m, "enter")
	if got := m.b.Task("t-9sa6").Due.Format("2006-01-02"); got != "2026-09-01" {
		t.Errorf("due = %s, want 2026-09-01", got)
	}
	drainPersists(m, t)

	// Empty input clears.
	m.edit.menuIdx = int(fieldDue)
	press(m, "enter")
	m.edit.input.SetValue("")
	press(m, "enter")
	if !m.b.Task("t-9sa6").Due.IsZero() {
		t.Error("an empty due must clear")
	}
	drainPersists(m, t)
}

func TestEditChecklistCursorTogglesTheSelectedItem(t *testing.T) {
	m := editModel(t, "t-9sa6") // 6 unchecked items
	m.edit.menuIdx = int(fieldChecklist)
	press(m, "enter", "down", "down") // cursor on item 2
	press(m, "x")
	cl := m.b.Task("t-9sa6").Checklist
	if !cl[2].Done || cl[0].Done {
		t.Errorf("x must toggle the SELECTED item, not the first unfinished: %+v", cl[:3])
	}
	drainPersists(m, t)

	// a appends.
	press(m, "a")
	m.edit.input.SetValue("新しい項目")
	press(m, "enter")
	cl = m.b.Task("t-9sa6").Checklist
	if cl[len(cl)-1].Text != "新しい項目" {
		t.Errorf("a must append, got %+v", cl[len(cl)-1])
	}
	drainPersists(m, t)

	// r rewords the selected item.
	m.edit.listIdx = 0
	press(m, "r")
	m.edit.input.SetValue("書き直した")
	press(m, "enter")
	if got := m.b.Task("t-9sa6").Checklist[0].Text; got != "書き直した" {
		t.Errorf("reword = %q", got)
	}
	drainPersists(m, t)

	// d deletes the selected item.
	n := len(m.b.Task("t-9sa6").Checklist)
	m.edit.listIdx = 0
	press(m, "d")
	cl = m.b.Task("t-9sa6").Checklist
	if len(cl) != n-1 || cl[0].Text == "書き直した" {
		t.Errorf("d must delete item 0: %d items, head %q", len(cl), cl[0].Text)
	}
	drainPersists(m, t)
}

func TestEditRetitleRefusesEmptyAndApplies(t *testing.T) {
	m := editModel(t, "t-9sa6")
	press(m, "enter") // menu row 0 = title
	m.edit.input.SetValue("   ")
	press(m, "enter")
	if strings.TrimSpace(m.b.Task("t-9sa6").Title) == "" {
		t.Error("an empty retitle must refuse")
	}
	press(m, "enter")
	m.edit.input.SetValue("新しいタイトル")
	press(m, "enter")
	if got := m.b.Task("t-9sa6").Title; got != "新しいタイトル" {
		t.Errorf("title = %q", got)
	}
	drainPersists(m, t)
}

func TestEditOverlayRendersInTheFrame(t *testing.T) {
	m := boardModel(t, 240, 50)
	if err := m.demoState("edit"); err != nil {
		t.Fatal(err)
	}
	out := frame(m)
	if !strings.Contains(out, "edit t-9sa6") {
		t.Error("the overlay header is missing")
	}
	if !strings.Contains(out, "⏎/x toggle · a add · d delete · r reword") {
		t.Error("the checklist stage hints are missing")
	}
	if !strings.Contains(out, "▌ [ ]") {
		t.Error("the checklist cursor is missing")
	}
}

func TestEditEscWalksBackOut(t *testing.T) {
	m := editModel(t, "t-9sa6")
	m.edit.menuIdx = int(fieldLabels)
	press(m, "enter") // list stage
	press(m, "esc")
	if m.edit.stage != stageMenu {
		t.Error("esc from a sub-editor must return to the menu")
	}
	press(m, "esc")
	if m.mode != modeNormal || m.edit != nil {
		t.Error("esc from the menu must close the overlay")
	}
}

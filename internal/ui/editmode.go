package ui

import (
	"fmt"
	"sort"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The field-edit overlay (t-5zjj): everything `furrow set/retitle/repo/check`
// can record, editable without leaving the board. GitHub's analogue is cell
// editing (Enter toggles it) plus the side panel's field controls; the panel
// itself is undocumented, so the shape here is ridge's own — one menu, one
// sub-editor per field, Esc walks back out.
//
// Every apply is optimistic: the board mutates first (board.SetFields and
// friends validate before touching anything), the write queues behind it, and
// a failed write rolls the whole board back via the persist queue's reload.

type editStage int

const (
	stageMenu  editStage = iota
	stagePick            // value/effort: a 1..5 keypress, 0 clears
	stageList            // labels / epic / repos / checklist cursor
	stageInput           // title / due / new label / new repo / checklist add+reword
)

type editField int

const (
	fieldTitle editField = iota
	fieldValue
	fieldEffort
	fieldLabels
	fieldEpic
	fieldDue
	fieldRepos
	fieldChecklist
	fieldCount // one past the last menu row
)

// inputKind says what the text input commits to.
type inputKind int

const (
	inputTitle inputKind = iota
	inputDue
	inputNewLabel
	inputNewRepo
	inputCheckAdd
	inputCheckReword
)

type editState struct {
	id       string
	stage    editStage
	field    editField
	menuIdx  int
	listIdx  int
	input    textinput.Model
	inputFor inputKind
}

// enterEdit opens the field-edit menu on the current selection.
func (m *Model) enterEdit() {
	t := m.curTask()
	if t == nil {
		m.note("nothing selected — the edit menu works on a task")
		return
	}
	m.cancelDrag()
	ti := textinput.New()
	ti.SetWidth(48)
	m.edit = &editState{id: t.ID, stage: stageMenu}
	m.edit.input = ti
	m.mode = modeEdit
	m.peekOpen = true
	m.syncPeek()
	m.note("edit %s — ⏎ pick a field · esc closes", t.ID)
}

func (m *Model) exitEdit() {
	m.mode = modeNormal
	m.edit = nil
}

// editTask resolves the task under edit; losing it (a reload dropped the id)
// closes the overlay rather than editing a ghost.
func (m *Model) editTask() *board.Task {
	if m.edit == nil {
		return nil
	}
	t := m.b.Task(m.edit.id)
	if t == nil {
		m.exitEdit()
	}
	return t
}

func (m *Model) onEditKey(msg tea.KeyPressMsg) tea.Cmd {
	t := m.editTask()
	if t == nil {
		return nil
	}
	e := m.edit
	if e.stage == stageInput {
		return m.onEditInputKey(msg, t)
	}

	switch {
	case key.Matches(msg, m.keys.Cancel):
		if e.stage == stageMenu {
			m.exitEdit()
		} else {
			e.stage, e.listIdx = stageMenu, 0
		}
		return nil
	case key.Matches(msg, m.keys.Quit) && e.stage == stageMenu:
		m.exitEdit()
		return nil
	}

	switch e.stage {
	case stageMenu:
		return m.onEditMenuKey(msg, t)
	case stagePick:
		return m.onEditPickKey(msg)
	case stageList:
		return m.onEditListKey(msg, t)
	}
	return nil
}

func (m *Model) onEditMenuKey(msg tea.KeyPressMsg, t *board.Task) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Up):
		m.edit.menuIdx = (m.edit.menuIdx + int(fieldCount) - 1) % int(fieldCount)
	case key.Matches(msg, m.keys.Down):
		m.edit.menuIdx = (m.edit.menuIdx + 1) % int(fieldCount)
	case key.Matches(msg, m.keys.Commit):
		return m.openField(editField(m.edit.menuIdx), t)
	}
	return nil
}

func (m *Model) openField(f editField, t *board.Task) tea.Cmd {
	e := m.edit
	e.field, e.listIdx = f, 0
	switch f {
	case fieldTitle:
		return m.startInput(inputTitle, t.Title, "title")
	case fieldValue, fieldEffort:
		e.stage = stagePick
	case fieldDue:
		cur := ""
		if !t.Due.IsZero() {
			cur = t.Due.Format("2006-01-02")
		}
		return m.startInput(inputDue, cur, "2026-08-04 · +1d · empty clears")
	case fieldLabels, fieldEpic, fieldRepos, fieldChecklist:
		e.stage = stageList
	}
	return nil
}

func (m *Model) startInput(kind inputKind, value, placeholder string) tea.Cmd {
	e := m.edit
	e.stage, e.inputFor = stageInput, kind
	e.input.SetValue(value)
	e.input.Placeholder = placeholder
	e.input.SetWidth(48)
	return e.input.Focus()
}

func (m *Model) onEditPickKey(msg tea.KeyPressMsg) tea.Cmd {
	s := msg.String()
	if len(s) != 1 || s[0] < '0' || s[0] > '5' {
		return nil
	}
	n := int(s[0] - '0')
	patch := board.FieldPatch{}
	label := ""
	if m.edit.field == fieldValue {
		patch.Value, label = &n, "value"
	} else {
		patch.Effort, label = &n, "effort"
	}
	m.edit.stage = stageMenu
	return m.applyPatch(label, patch)
}

func (m *Model) onEditListKey(msg tea.KeyPressMsg, t *board.Task) tea.Cmd {
	e := m.edit
	rows := len(m.editListRows(t))
	switch {
	case key.Matches(msg, m.keys.Up):
		if rows > 0 {
			e.listIdx = (e.listIdx + rows - 1) % rows
		}
	case key.Matches(msg, m.keys.Down):
		if rows > 0 {
			e.listIdx = (e.listIdx + 1) % rows
		}

	case key.Matches(msg, m.keys.Commit), key.Matches(msg, m.keys.Check):
		return m.editListSelect(t)

	case msg.String() == "a":
		switch e.field {
		case fieldLabels:
			return m.startInput(inputNewLabel, "", "new label")
		case fieldRepos:
			return m.startInput(inputNewRepo, "", "owner/repo")
		case fieldChecklist:
			return m.startInput(inputCheckAdd, "", "new checklist item")
		}

	case msg.String() == "d" && e.field == fieldChecklist:
		if e.listIdx < len(t.Checklist) {
			i := e.listIdx
			if e.listIdx >= len(t.Checklist)-1 {
				e.listIdx = maxInt(0, len(t.Checklist)-2)
			}
			return m.applyCheck("check rm", func() error { return m.b.CheckRm(t.ID, i) },
				func() error { return m.prov.PersistCheckRm(t.ID, i) })
		}

	case msg.String() == "r" && e.field == fieldChecklist:
		if e.listIdx < len(t.Checklist) {
			return m.startInput(inputCheckReword, t.Checklist[e.listIdx].Text, "reworded item")
		}
	}
	return nil
}

// editListSelect is Enter/x on a list row: toggle a label or repo, file under
// an epic, or toggle a checklist item.
func (m *Model) editListSelect(t *board.Task) tea.Cmd {
	e := m.edit
	rows := m.editListRows(t)
	if e.listIdx >= len(rows) {
		return nil
	}
	val := rows[e.listIdx]
	switch e.field {
	case fieldLabels:
		p := board.FieldPatch{AddLabels: []string{val}}
		if containsStrUI(t.Labels, val) {
			p = board.FieldPatch{RmLabels: []string{val}}
		}
		return m.applyPatch("label", p)
	case fieldRepos:
		p := board.FieldPatch{AddRepos: []string{val}}
		if containsStrUI(t.Repos, val) {
			p = board.FieldPatch{RmRepos: []string{val}}
		}
		return m.applyPatch("repo", p)
	case fieldEpic:
		id := ""
		if e.listIdx > 0 {
			id = m.b.Epics()[e.listIdx-1].ID
		}
		e.stage = stageMenu
		return m.applyPatch("epic", board.FieldPatch{Epic: &id})
	case fieldChecklist:
		i := e.listIdx
		return m.applyCheck("check", func() error { return m.b.ToggleCheck(t.ID, i) },
			func() error {
				return m.prov.PersistCheck(t.ID, i, m.b.Task(t.ID).Checklist[i].Done)
			})
	}
	return nil
}

// editListRows is the current list-stage rows, as raw values (labels, repo
// names, epic titles, checklist texts).
func (m *Model) editListRows(t *board.Task) []string {
	switch m.edit.field {
	case fieldLabels:
		return vocabUnion(m.labelVocab(), t.Labels)
	case fieldRepos:
		return vocabUnion(m.repoVocab(), t.Repos)
	case fieldEpic:
		rows := []string{"(unfiled)"}
		for _, e := range m.b.Epics() {
			rows = append(rows, e.ID+" "+e.Title)
		}
		return rows
	case fieldChecklist:
		var rows []string
		for _, c := range t.Checklist {
			rows = append(rows, c.Text)
		}
		return rows
	}
	return nil
}

func (m *Model) onEditInputKey(msg tea.KeyPressMsg, t *board.Task) tea.Cmd {
	e := m.edit
	switch {
	case key.Matches(msg, m.keys.Cancel):
		e.input.Blur()
		if e.inputFor == inputTitle || e.inputFor == inputDue {
			e.stage = stageMenu
		} else {
			e.stage = stageList
		}
		return nil
	case key.Matches(msg, m.keys.Commit):
		v := strings.TrimSpace(e.input.Value())
		e.input.Blur()
		switch e.inputFor {
		case inputTitle:
			e.stage = stageMenu
			return m.applyPatch("retitle", board.FieldPatch{Title: &v})
		case inputDue:
			e.stage = stageMenu
			return m.applyPatch("due", board.FieldPatch{Due: &v})
		case inputNewLabel:
			e.stage = stageList
			if v == "" {
				return nil
			}
			return m.applyPatch("label", board.FieldPatch{AddLabels: []string{v}})
		case inputNewRepo:
			e.stage = stageList
			if v == "" {
				return nil
			}
			return m.applyPatch("repo", board.FieldPatch{AddRepos: []string{v}})
		case inputCheckAdd:
			e.stage = stageList
			if v == "" {
				return nil
			}
			return m.applyCheck("check add", func() error { return m.b.CheckAdd(t.ID, v) },
				func() error { return m.prov.PersistCheckAdd(t.ID, v) })
		case inputCheckReword:
			e.stage = stageList
			i := e.listIdx
			return m.applyCheck("check reword", func() error { return m.b.CheckReword(t.ID, i, v) },
				func() error { return m.prov.PersistCheckReword(t.ID, i, v) })
		}
		return nil
	}
	var c tea.Cmd
	e.input, c = e.input.Update(msg)
	return c
}

// applyPatch is the one funnel for a field edit: local apply (validated),
// re-render, then the queued store write.
func (m *Model) applyPatch(label string, p board.FieldPatch) tea.Cmd {
	id := m.edit.id
	if err := m.b.SetFields(id, p); err != nil {
		m.fail("%v", err)
		return nil
	}
	m.recompute()
	m.syncPeek()
	m.note("%s %s", label, id)
	return m.enqueuePersist(label+" "+id, func() ([]string, error) {
		return nil, m.prov.PersistFields(id, p)
	})
}

// applyCheck is applyPatch's twin for the checklist ops, whose local applies
// are not FieldPatch-shaped.
func (m *Model) applyCheck(label string, local func() error, persist func() error) tea.Cmd {
	id := m.edit.id
	if err := local(); err != nil {
		m.fail("%v", err)
		return nil
	}
	m.recompute()
	m.syncPeek()
	return m.enqueuePersist(label+" "+id, func() ([]string, error) { return nil, persist() })
}

// labelVocab is every label on the board, sorted — the toggle list's rows.
func (m *Model) labelVocab() []string {
	set := map[string]bool{}
	for _, t := range m.b.Tasks() {
		for _, l := range t.Labels {
			set[l] = true
		}
	}
	return sortedKeys(set)
}

// repoVocab is every repo attached anywhere on the board, sorted.
func (m *Model) repoVocab() []string {
	set := map[string]bool{}
	for _, t := range m.b.Tasks() {
		for _, r := range t.Repos {
			set[r] = true
		}
	}
	return sortedKeys(set)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func vocabUnion(vocab, own []string) []string {
	set := map[string]bool{}
	for _, v := range vocab {
		set[v] = true
	}
	for _, v := range own {
		set[v] = true
	}
	return sortedKeys(set)
}

func containsStrUI(hay []string, needle string) bool {
	for _, h := range hay {
		if h == needle {
			return true
		}
	}
	return false
}

// ---- rendering --------------------------------------------------------------

// editLayer draws the overlay: the menu with current values, or the active
// sub-editor. Same Layer style as the peek and help overlays.
func (m *Model) editLayer() *lg.Layer {
	th := m.th
	t := m.b.Task(m.edit.id)
	if t == nil {
		return nil
	}
	inner := clamp(m.w/3, 44, 72)

	var body string
	switch m.edit.stage {
	case stageMenu:
		body = m.renderEditMenu(t, inner)
	case stagePick:
		name := "value"
		if m.edit.field == fieldEffort {
			name = "effort"
		}
		body = th.peekHdr.Render("set "+name) + "\n\n" +
			pad("press 1-5 · 0 clears · esc back", inner)
	case stageList:
		body = m.renderEditList(t, inner)
	case stageInput:
		body = th.peekHdr.Render(inputTitleFor(m.edit.inputFor)) + "\n\n" +
			m.edit.input.View() + "\n" + th.dim.Render(pad("⏎ apply · esc back", inner))
	}

	box := th.peek.Render(pad(th.peekHdr.Render("edit "+t.ID), inner) + "\n\n" + body)
	box = lg.NewStyle().MaxWidth(m.w).MaxHeight(m.h).Render(box)
	x := maxInt(0, (m.w-lg.Width(box))/2)
	y := maxInt(0, (m.h-lg.Height(box))/3)
	return lg.NewLayer(box).ID("edit").X(x).Y(y).Z(zEdit)
}

func inputTitleFor(k inputKind) string {
	switch k {
	case inputTitle:
		return "retitle"
	case inputDue:
		return "due date"
	case inputNewLabel:
		return "add label"
	case inputNewRepo:
		return "attach repo"
	case inputCheckAdd:
		return "add checklist item"
	case inputCheckReword:
		return "reword checklist item"
	}
	return ""
}

func (m *Model) renderEditMenu(t *board.Task, inner int) string {
	th := m.th
	est := func(n int) string {
		if n == 0 {
			return "—"
		}
		return fmt.Sprintf("%d", n)
	}
	epicLabel := "—"
	if t.Epic != "" {
		epicLabel = t.Epic
		if e := m.b.Epic(t.Epic); e != nil {
			epicLabel = ansi.Truncate(e.Title, 24, "…")
		}
	}
	due := "—"
	if !t.Due.IsZero() {
		due = t.Due.Format("2006-01-02")
	}
	cd, ct := t.CheckProgress()
	rows := []struct{ name, cur string }{
		{"title", ansi.Truncate(t.Title, inner-14, "…")},
		{"value", est(t.Value)},
		{"effort", est(t.Effort)},
		{"labels", strings.Join(t.Labels, ",")},
		{"epic", epicLabel},
		{"due", due},
		{"repos", ansi.Truncate(strings.Join(t.Repos, ","), inner-14, "…")},
		{"checklist", fmt.Sprintf("%d/%d", cd, ct)},
	}
	var b strings.Builder
	for i, r := range rows {
		cursor, style := "  ", th.base
		if i == m.edit.menuIdx {
			cursor, style = "▌ ", th.peekHdr
		}
		cur := r.cur
		if cur == "" {
			cur = "—"
		}
		b.WriteString(cursor + style.Render(pad(r.name, 10)) + th.muted.Render(pad(cur, maxInt(4, inner-13))) + "\n")
	}
	b.WriteString("\n" + th.dim.Render(pad("↑↓ field · ⏎ edit · esc close", inner)))
	return b.String()
}

func (m *Model) renderEditList(t *board.Task, inner int) string {
	th := m.th
	e := m.edit
	rows := m.editListRows(t)

	hdr, foot := "", ""
	mark := func(i int, row string) string { return row }
	switch e.field {
	case fieldLabels:
		hdr, foot = "labels", "⏎/x toggle · a new label · esc back"
		mark = func(_ int, row string) string {
			box := "[ ] "
			if containsStrUI(t.Labels, row) {
				box = "[x] "
			}
			return box + row
		}
	case fieldRepos:
		hdr, foot = "repos", "⏎/x attach/detach · a new repo · esc back"
		mark = func(_ int, row string) string {
			box := "[ ] "
			if containsStrUI(t.Repos, row) {
				box = "[x] "
			}
			return box + row
		}
	case fieldEpic:
		hdr, foot = "file under epic", "⏎ select · esc back"
		mark = func(i int, row string) string {
			cur := i == 0 && t.Epic == "" ||
				i > 0 && t.Epic != "" && strings.HasPrefix(row, t.Epic+" ")
			if cur {
				return "● " + row
			}
			return "  " + row
		}
	case fieldChecklist:
		hdr, foot = "checklist", "⏎/x toggle · a add · d delete · r reword · esc back"
		mark = func(i int, row string) string {
			box := "[ ] "
			if t.Checklist[i].Done {
				box = "[x] "
			}
			return box + row
		}
	}

	var b strings.Builder
	b.WriteString(th.peekHdr.Render(hdr) + "\n\n")
	if len(rows) == 0 {
		b.WriteString(th.dim.Render(pad("(empty)", inner)) + "\n")
	}
	for i, row := range rows {
		cursor, style := "  ", th.base
		if i == e.listIdx {
			cursor, style = "▌ ", th.peekHdr
		}
		b.WriteString(cursor + style.Render(pad(ansi.Truncate(mark(i, row), inner-2, "…"), inner-2)) + "\n")
	}
	b.WriteString("\n" + th.dim.Render(pad(foot, inner)))
	return b.String()
}

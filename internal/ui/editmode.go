package ui

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The field-edit overlay (t-5zjj): everything `furrow set/retitle/repo/check`
// can record, editable without leaving the board. GitHub's analogue is cell
// editing (Enter toggles it) plus the side panel's field controls; the panel
// itself is undocumented, so the shape here is ridge's own — one menu, one
// sub-editor per field, Esc walks back out. The walk itself is overlayShell's
// (overlay.go); this file is what each field means.
//
// Every apply is optimistic: the board mutates first (board.SetFields and
// friends validate before touching anything), the write queues behind it, and
// a failed write rolls the whole board back via the persist queue's reload.

type editField int

const (
	fieldTitle editField = iota
	fieldValue
	fieldEffort
	fieldLabels
	fieldEpic
	fieldDue
	fieldDeps
	fieldRepos
	fieldRefs
	fieldChecklist
	fieldCount // one past the last menu row
)

// editFieldName is the menu row's label, spelled once: the menu renders it and
// the status row names the field the sub-editor is on.
func editFieldName(f editField) string {
	switch f {
	case fieldTitle:
		return "title"
	case fieldValue:
		return "value"
	case fieldEffort:
		return "effort"
	case fieldLabels:
		return "labels"
	case fieldEpic:
		return "epic"
	case fieldDue:
		return "due"
	case fieldDeps:
		return "deps"
	case fieldRepos:
		return "repos"
	case fieldRefs:
		return "refs"
	case fieldChecklist:
		return "checklist"
	}
	return ""
}

// inputKind says what the text input commits to.
type inputKind int

const (
	inputTitle inputKind = iota
	inputDue
	inputNewLabel
	inputNewRepo
	inputNewRef
	inputCheckAdd
	inputCheckReword
	inputDepAdd
	// inputNote is the one input the overlay opens DIRECTLY (enterNote): there
	// is no list or menu behind it, so both esc and a landed apply close the
	// whole overlay instead of walking back a stage.
	inputNote
)

type editState = overlayShell[editField, inputKind]

// editHooks is the task overlay's half of the stage machine: the shell walks
// the stages, these say what each one does to the task under edit.
type editHooks struct {
	m *Model
	t *board.Task
}

func (h editHooks) note()              { h.m.noteEditStage() }
func (h editHooks) exit()              { h.m.exitEdit() }
func (h editHooks) subject() string    { return "edit " + h.t.ID }
func (h editHooks) fieldCount() int    { return int(fieldCount) }
func (h editHooks) listRows() []string { return h.m.editListRows(h.t) }

func (h editHooks) openField(f editField) tea.Cmd       { return h.m.openField(f, h.t) }
func (h editHooks) listSelect(rows []string) tea.Cmd    { return h.m.editListSelect(h.t, rows) }
func (h editHooks) listKey(msg tea.KeyPressMsg) tea.Cmd { return h.m.onEditListKey(msg, h.t) }
func (h editHooks) gateKey(msg tea.KeyPressMsg) tea.Cmd { return h.m.onEditPickKey(msg) }
func (h editHooks) inputCancel(k inputKind) tea.Cmd     { return h.m.onEditInputCancel(k) }
func (h editHooks) inputCommit(k inputKind, v string) tea.Cmd {
	return h.m.onEditInputCommit(k, v, h.t)
}

// enterEdit opens the field-edit menu on the current selection.
func (m *Model) enterEdit() {
	t := m.curTask()
	if t == nil {
		m.note("nothing selected — the edit menu works on a task")
		return
	}
	m.cancelDrag()
	m.edit = &editState{id: t.ID, stage: stageMenu, input: newOverlayInput()}
	m.mode = modeEdit
	m.peekOpen = true
	m.syncPeek()
	m.noteEditStage()
}

// enterNote opens the note input directly on the current selection — the
// light path for one paragraph of progress, next to `e`'s full $EDITOR. It
// reuses the edit overlay's input stage rather than growing a mode: the
// overlay already owns the keyboard, the esc route and the apply funnel.
// The peek opens with it so the appended paragraph is visibly landing in the
// body, the same reason enterEdit opens it.
func (m *Model) enterNote() tea.Cmd {
	t := m.curTask()
	if t == nil {
		m.note("nothing selected — a note appends to a task")
		return nil
	}
	m.cancelDrag()
	m.edit = &editState{id: t.ID, stage: stageInput, input: newOverlayInput()}
	m.mode = modeEdit
	m.peekOpen = true
	m.syncPeek()
	return m.edit.startInput(editHooks{m, t}, inputNote, "", "progress in one paragraph — appended to the body")
}

func (m *Model) exitEdit() {
	m.mode = modeNormal
	m.edit = nil
}

// noteEditStage keeps the bottom row true as the overlay moves between stages.
// enterEdit wrote it once and nothing re-wrote it, so every sub-editor still
// advertised "⏎ pick a field · esc closes" — while in stageList ⏎ was toggling
// a checklist item and esc went BACK to the menu, and in stageGate ⏎ was a
// literal no-op. It is the only note in the app that made an imperative key
// claim false at read time, which is the failure the one-row status exists to
// prevent. Every other mode already re-notes on state change.
func (m *Model) noteEditStage() {
	e := m.edit
	if e == nil {
		return
	}
	// Never over-write a refusal nobody has read yet. applyPatch reports a
	// rejected edit through m.fail, and the stage change that follows would
	// erase it — the same failure cancelMove was taught to avoid in this
	// branch, and the reason the status row exists at all.
	if m.statusErr {
		return
	}
	switch e.stage {
	case stageGate:
		m.note("edit %s · %s — 1-5 sets · 0 clears · esc back", e.id, editFieldName(e.field))
	case stageList:
		if e.field == fieldDeps || e.field == fieldRefs {
			// A dep or ref row only points one way — selecting REMOVES it —
			// so "toggle" would promise a re-add the list cannot do (the
			// vocabulary of potential deps/refs is not a togglable list).
			m.note("edit %s · %s — ⏎/x remove · a add · esc back", e.id, editFieldName(e.field))
		} else {
			m.note("edit %s · %s — ⏎/x toggle · esc back", e.id, editFieldName(e.field))
		}
	case stageInput:
		m.note("edit %s · %s — ⏎ apply · esc back", e.id, inputTitleFor(e.inputFor))
	default:
		m.note("edit %s — ⏎ pick a field · esc closes", e.id)
	}
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
	// ctrl+c quits from every stage — in the input stages `q` types, and
	// bubbletea's raw mode delivers ctrl+c as an ordinary keystroke, so
	// nothing else would ever let go of the keyboard. Checked BEFORE
	// editTask: when a reconcile dropped the task, editTask closes the
	// overlay and would swallow this very keystroke. In stageMenu too it
	// quits the app, deliberately — not merely the overlay.
	if key.Matches(msg, m.keys.ForceQuit) {
		return m.quitOrFlush()
	}
	t := m.editTask()
	if t == nil {
		return nil
	}
	return m.edit.onKey(m, msg, editHooks{m, t})
}

func (m *Model) openField(f editField, t *board.Task) tea.Cmd {
	e, h := m.edit, editHooks{m, t}
	e.field, e.listIdx = f, 0
	switch f {
	case fieldTitle:
		return e.startInput(h, inputTitle, t.Title, "title")
	case fieldValue, fieldEffort:
		e.stage = stageGate
		m.noteEditStage()
	case fieldDue:
		cur := ""
		if !t.Due.IsZero() {
			cur = t.Due.In(localZone()).Format("2006-01-02")
		}
		return e.startInput(h, inputDue, cur, "2026-08-04 · +1d · +2h · empty clears")
	case fieldLabels, fieldEpic, fieldDeps, fieldRepos, fieldRefs, fieldChecklist:
		e.stage = stageList
		if f == fieldEpic {
			// Epic is the one list whose row 0 DESTROYS a field: selecting
			// "(unfiled)" persists `furrow set -e ""`. Every other stage's row 0
			// is a reversible toggle, so opening on it is harmless there and is
			// a trap here — open on the task's current epic instead, and only
			// fall back to row 0 when the task really is unfiled.
			e.listIdx = epicRow(m.epicPickList(t.Epic), t.Epic)
		}
		m.noteEditStage()
	}
	return nil
}

// epicPickList is the population the epic picker offers, and the ONE source
// its three index users share — the rows, the cursor's landing row, and the
// commit that reads the row back. Getting them from different slices is how the
// cursor ends up pointing at a different box than the one it renders.
//
// It is the OPEN boxes, plus the task's own box when that box is CLOSED.
// furrow permits a task to stay filed under a closed box — it lints it
// epic-closed, a warning, not an error — and a picker that cannot represent the
// current value lands its cursor on row 0, "(unfiled)", whose ⏎ silently
// unfiles the task. That is the exact trap the landing rule below exists for.
func (m *Model) epicPickList(cur string) []board.EpicInfo {
	open := m.b.Epics()
	if cur == "" {
		return open
	}
	for _, e := range open {
		if e.ID == cur {
			return open
		}
	}
	box := m.b.Epic(cur)
	if box == nil {
		return open // a membership no read serves: nothing to represent
	}
	return append(append([]board.EpicInfo(nil), open...), *box)
}

// epicRow is the list-stage row index of an epic membership. Row 0 is
// "(unfiled)", so a filed task sits at its position in the pick list plus one;
// an id the board cannot resolve (a stale membership) falls back to row 0.
func epicRow(epics []board.EpicInfo, id string) int {
	if id == "" {
		return 0
	}
	for i, e := range epics {
		if e.ID == id {
			return i + 1
		}
	}
	return 0
}

// onEditPickKey is the gate stage: value / effort take a 1..5 keypress, 0
// clears, anything else is ignored.
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

// onEditListKey is the list stage's own keys, after the shell has answered the
// cursor and ⏎/x: `a` adds to the one-way lists, `d` / `r` work the checklist.
func (m *Model) onEditListKey(msg tea.KeyPressMsg, t *board.Task) tea.Cmd {
	e, h := m.edit, editHooks{m, t}
	switch {
	case msg.String() == "a":
		switch e.field {
		case fieldLabels:
			return e.startInput(h, inputNewLabel, "", "new label")
		case fieldDeps:
			return e.startInput(h, inputDepAdd, "", "t-… — the task this waits on")
		case fieldRepos:
			return e.startInput(h, inputNewRepo, "", "owner/repo")
		case fieldRefs:
			return e.startInput(h, inputNewRef, "", "file:line or URL")
		case fieldChecklist:
			return e.startInput(h, inputCheckAdd, "", "new checklist item")
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
			return e.startInput(h, inputCheckReword, t.Checklist[e.listIdx].Text, "reworded item")
		}
	}
	return nil
}

// editListSelect is Enter/x on a list row: toggle a label or repo, file under
// an epic, or toggle a checklist item.
func (m *Model) editListSelect(t *board.Task, rows []string) tea.Cmd {
	e := m.edit
	if e.listIdx >= len(rows) {
		return nil
	}
	val := rows[e.listIdx]
	switch e.field {
	case fieldLabels:
		p := board.FieldPatch{AddLabels: []string{val}}
		if slices.Contains(t.Labels, val) {
			p = board.FieldPatch{RmLabels: []string{val}}
		}
		return m.applyPatch("label", p)
	case fieldRepos:
		p := board.FieldPatch{AddRepos: []string{val}}
		if slices.Contains(t.Repos, val) {
			p = board.FieldPatch{RmRepos: []string{val}}
		}
		return m.applyPatch("repo", p)
	case fieldEpic:
		id := ""
		if picks := m.epicPickList(t.Epic); e.listIdx > 0 && e.listIdx <= len(picks) {
			id = picks[e.listIdx-1].ID
		}
		e.stage = stageMenu
		return m.applyPatch("epic", board.FieldPatch{Epic: &id})
	case fieldDeps:
		// Every row IS a dep, so the toggle only points one way: selecting
		// removes the edge. Re-adding is `a`, because the vocabulary of
		// potential deps is the whole board, not a togglable list.
		if e.listIdx >= len(t.Deps)-1 {
			e.listIdx = maxInt(0, len(t.Deps)-2)
		}
		return m.applyCheck("dep rm", func() error { return m.b.DepRm(t.ID, val) },
			func() error { return m.prov.PersistDepRm(t.ID, val) })
	case fieldRefs:
		// Same one-way list as deps: every row IS a ref, selecting removes it,
		// `a` adds — a ref is free text, not a togglable vocabulary.
		if e.listIdx >= len(t.Refs)-1 {
			e.listIdx = maxInt(0, len(t.Refs)-2)
		}
		return m.applyPatch("ref rm", board.FieldPatch{RmRefs: []string{val}})
	case fieldChecklist:
		i := e.listIdx
		if i >= len(t.Checklist) {
			return nil
		}
		// Captured NOW, on the UI thread — the persist closure runs at drain
		// time, when the checklist may have shrunk or the task vanished, so
		// it must not read the live board (that read was both an index-panic
		// and a data race; the provider contract says persists carry values).
		done := !t.Checklist[i].Done
		return m.applyCheck("check", func() error { return m.b.ToggleCheck(t.ID, i) },
			func() error { return m.prov.PersistCheck(t.ID, i, done) })
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
		for _, e := range m.epicPickList(t.Epic) {
			row := e.ID + " " + e.Title
			if !e.Closed.IsZero() {
				row += " (closed)"
			}
			rows = append(rows, row)
		}
		return rows
	case fieldDeps:
		// A copy, not the live slice: DepRm's slices.DeleteFunc shrinks the backing
		// array in place, so a caller holding these rows across a removal
		// would read zeroed tails.
		return append([]string(nil), t.Deps...)
	case fieldRefs:
		// A copy for the same reason: SetFields' RmRefs shrinks in place.
		return append([]string(nil), t.Refs...)
	case fieldChecklist:
		var rows []string
		for _, c := range t.Checklist {
			rows = append(rows, c.Text)
		}
		return rows
	}
	return nil
}

// onEditInputCancel is esc in the input: back to the stage the input was
// opened from.
func (m *Model) onEditInputCancel(k inputKind) tea.Cmd {
	e := m.edit
	switch k {
	case inputNote:
		// There is no stage behind this input — enterNote opened the
		// overlay straight onto it — so esc closes the overlay, not a
		// menu the user never saw.
		m.exitEdit()
		m.note("note cancelled — nothing appended")
		return nil
	case inputTitle, inputDue:
		e.stage = stageMenu
	default:
		e.stage = stageList
	}
	m.noteEditStage()
	return nil
}

// onEditInputCommit is ⏎ in the input, v already trimmed and already past the
// rolling-back refusal. Every branch that lands back in stageList re-notes,
// because there ⏎ TOGGLES or REMOVES — a board write — and the input's
// surviving "⏎ apply" would be the false-key-claim failure noteEditStage exists
// to prevent (a refusal survives the re-note: noteEditStage never over-writes an
// unread m.fail). The branches whose apply writes its own status ("ref <id>",
// "retitle <id>") need no re-note.
func (m *Model) onEditInputCommit(k inputKind, v string, t *board.Task) tea.Cmd {
	e := m.edit
	switch k {
	case inputTitle:
		e.stage = stageMenu
		return m.applyPatch("retitle", board.FieldPatch{Title: &v})
	case inputDue:
		e.stage = stageMenu
		return m.applyPatch("due", board.FieldPatch{Due: &v})
	case inputNewLabel:
		e.stage = stageList
		if v == "" {
			m.noteEditStage()
			return nil
		}
		return m.applyPatch("label", board.FieldPatch{AddLabels: []string{v}})
	case inputNewRepo:
		e.stage = stageList
		if v == "" {
			m.noteEditStage()
			return nil
		}
		return m.applyPatch("repo", board.FieldPatch{AddRepos: []string{v}})
	case inputNewRef:
		e.stage = stageList
		if v == "" {
			m.noteEditStage()
			return nil
		}
		return m.applyPatch("ref", board.FieldPatch{AddRefs: []string{v}})
	case inputCheckAdd:
		e.stage = stageList
		if v == "" {
			m.noteEditStage()
			return nil
		}
		cmd := m.applyCheck("check add", func() error { return m.b.CheckAdd(t.ID, v) },
			func() error { return m.prov.PersistCheckAdd(t.ID, v) })
		m.noteEditStage()
		return cmd
	case inputCheckReword:
		e.stage = stageList
		i := e.listIdx
		cmd := m.applyCheck("check reword", func() error { return m.b.CheckReword(t.ID, i, v) },
			func() error { return m.prov.PersistCheckReword(t.ID, i, v) })
		m.noteEditStage()
		return cmd
	case inputDepAdd:
		e.stage = stageList
		if v == "" {
			m.noteEditStage()
			return nil
		}
		// Without the re-note the NEXT ⏎ silently deletes the dep under the
		// cursor — in stageList ⏎ removes an edge, the one destructive key in
		// the overlay.
		cmd := m.applyCheck("dep add", func() error { return m.b.DepAdd(t.ID, v) },
			func() error { return m.prov.PersistDepAdd(t.ID, v) })
		m.noteEditStage()
		return cmd
	case inputNote:
		if v == "" {
			// Nothing to append; leave like esc does. AppendNote would
			// refuse it anyway (furrow's "note text is empty"), but an
			// empty ⏎ is a back-out, not a mistake to report as one.
			m.exitEdit()
			m.note("note cancelled — nothing appended")
			return nil
		}
		// One paragraph per open: the apply CLOSES the overlay (there is
		// no list to land back in), and the peek left open behind it shows
		// the paragraph at the body's tail.
		cmd := m.applyCheck("note", func() error { return m.b.AppendNote(t.ID, v) },
			func() error { return m.prov.PersistNote(t.ID, v) })
		if cmd == nil {
			// The LOCAL apply refused (the shell's rolling-back refusal never
			// reaches applyCheck): the fail is on the status row and the
			// typed text is still in the input — re-focus it (the shell
			// blurred it before this) instead of closing over hand-typed
			// prose.
			return e.input.Focus()
		}
		m.exitEdit()
		m.note("note appended to %s", t.ID)
		return cmd
	}
	return nil
}

// applyPatch is the one funnel for a field edit: local apply (validated),
// re-render, then the queued store write.
func (m *Model) applyPatch(label string, p board.FieldPatch) tea.Cmd {
	id := m.edit.id
	// Every field gesture in the overlay funnels through here — title, due,
	// value, effort, epic, and the label/repo/ref list rows — so this one
	// guard is the whole overlay's up-front refusal.
	if m.refuseWhileRollingBack(label + " " + id) {
		return nil
	}
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

// applyCheck is applyPatch's twin for the checklist and dep ops, whose local
// applies are not FieldPatch-shaped.
func (m *Model) applyCheck(label string, local func() error, persist func() error) tea.Cmd {
	id := m.edit.id
	if m.refuseWhileRollingBack(label + " " + id) {
		return nil
	}
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
	return slices.Sorted(maps.Keys(set))
}

// repoVocab is every repo attached anywhere on the board, sorted.
func (m *Model) repoVocab() []string {
	set := map[string]bool{}
	for _, t := range m.b.Tasks() {
		for _, r := range t.Repos {
			set[r] = true
		}
	}
	return slices.Sorted(maps.Keys(set))
}

func vocabUnion(vocab, own []string) []string {
	set := map[string]bool{}
	for _, v := range vocab {
		set[v] = true
	}
	for _, v := range own {
		set[v] = true
	}
	return slices.Sorted(maps.Keys(set))
}

// ---- rendering --------------------------------------------------------------

// editLayer draws the overlay: the menu with current values, or the active
// sub-editor.
func (m *Model) editLayer() *lg.Layer {
	th := m.th
	t := m.b.Task(m.edit.id)
	if t == nil {
		return nil
	}
	inner := m.overlayInner()

	var body string
	switch m.edit.stage {
	case stageMenu:
		body = m.renderEditMenu(t, inner)
	case stageGate:
		body = th.peekHdr.Render("set "+editFieldName(m.edit.field)) + "\n\n" +
			pad("press 1-5 · 0 clears · esc back", inner)
	case stageList:
		body = m.renderEditList(t, inner, maxInt(1, m.h-overlayListChrome))
	case stageInput:
		body = m.renderOverlayInput(inputTitleFor(m.edit.inputFor), m.edit.input, inner)
	}
	return m.overlayLayer("edit", "edit "+t.ID, body)
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
	case inputNewRef:
		return "add ref — file:line or URL"
	case inputCheckAdd:
		return "add checklist item"
	case inputCheckReword:
		return "reword checklist item"
	case inputDepAdd:
		return "add dep — this task will wait on it"
	case inputNote:
		return "append note — one paragraph onto the body"
	}
	return ""
}

func (m *Model) renderEditMenu(t *board.Task, inner int) string {
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
			// A closed box resolves now, so without this the membership reads
			// like any other and the one thing worth noticing about it — that
			// furrow lints it, epic-closed — is the only thing not shown.
			if !e.Closed.IsZero() {
				epicLabel += " (closed)"
			}
		}
	}
	due := "—"
	if !t.Due.IsZero() {
		due = t.Due.In(localZone()).Format("2006-01-02")
	}
	cd, ct := t.CheckProgress()
	rows := []menuRow{
		{editFieldName(fieldTitle), t.Title},
		{editFieldName(fieldValue), est(t.Value)},
		{editFieldName(fieldEffort), est(t.Effort)},
		{editFieldName(fieldLabels), strings.Join(t.Labels, ",")},
		{editFieldName(fieldEpic), epicLabel},
		{editFieldName(fieldDue), due},
		{editFieldName(fieldDeps), strings.Join(t.Deps, ",")},
		{editFieldName(fieldRepos), strings.Join(t.Repos, ",")},
		// Refs are free text and may carry a comma (furrow #317), so the
		// summary joins on a middle dot — a comma here would read as a split.
		{editFieldName(fieldRefs), strings.Join(t.Refs, " · ")},
		{editFieldName(fieldChecklist), fmt.Sprintf("%d/%d", cd, ct)},
	}
	return m.renderOverlayMenu(rows, m.edit.menuIdx, inner)
}

func (m *Model) renderEditList(t *board.Task, inner, budget int) string {
	e := m.edit
	rows := m.editListRows(t)

	hdr, foot := "", ""
	mark := func(_ int, row string) string { return row }
	switch e.field {
	case fieldLabels:
		hdr, foot = "labels", "⏎/x toggle · a new label · esc back"
		mark = func(_ int, row string) string {
			box := "[ ] "
			if slices.Contains(t.Labels, row) {
				box = "[x] "
			}
			return box + row
		}
	case fieldRepos:
		hdr, foot = "repos", "⏎/x attach/detach · a new repo · esc back"
		mark = func(_ int, row string) string {
			box := "[ ] "
			if slices.Contains(t.Repos, row) {
				box = "[x] "
			}
			return box + row
		}
	case fieldDeps:
		hdr, foot = "deps — waits on", "⏎/x remove · a add · esc back"
		mark = func(_ int, row string) string {
			// The same state glyphs as the peek's dep lines: o open, v done,
			// ? not on this board.
			g := glyphOpen
			switch {
			case m.b.Task(row) == nil:
				g = glyphUnknown
			case m.g.IsDone(row):
				g = glyphDone
			}
			label := row
			if dep := m.b.Task(row); dep != nil {
				label = row + " " + dep.Title
			}
			return g + " " + label
		}
	case fieldRefs:
		// Plain rows: a ref is free text (file:line or URL) with no state to
		// glyph and no vocabulary to check off.
		hdr, foot = "refs", "⏎/x remove · a add · esc back"
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
	return m.renderOverlayList(hdr, foot, rows, e.listIdx, inner, budget, mark)
}

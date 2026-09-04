package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The stage machine the task-edit overlay (editmode.go) and the epic overlay
// (epicmode.go) share: a field menu, one sub-editor per field — a cursor over a
// list, a text input, or a one-keystroke gate — and esc walking back out.
//
// overlayShell holds the walk's state and performs the walk; what a field MEANS
// is the overlay's, reached through overlayHooks. The shell reads no task and no
// box and issues no write; the one status row it writes itself is the
// rolling-back refusal of a typed input (onInputKey) — every other note is the
// hooks', at the moments both overlays agree on. Anything true of only one
// overlay (the epic's creating modal, its confirm wording, its store-first
// refusals; the task's note input that closes the whole overlay) stays in that
// overlay.
//
// F is the overlay's field enum (its menu rows, 0..fieldCount-1) and K its
// input-kind enum (what the text input commits to).

type overlayStage int

const (
	stageMenu  overlayStage = iota
	stageGate               // one keystroke between the menu and a write: the task's 1..5 pick, the box's ⏎ confirm
	stageList               // a cursor over rows
	stageInput              // a text input
)

type overlayShell[F ~int, K ~int] struct {
	// id is the task or box under edit, held for the overlay's whole life.
	// Never a row INDEX: a reload can move the row out from under the cursor,
	// and so can the overlay's own write — furrow orders `epic ls`
	// active-first, so activating a box reorders the very list it sits in.
	id       string
	stage    overlayStage
	field    F
	menuIdx  int
	listIdx  int
	input    textinput.Model
	inputFor K
}

// overlayHooks is the overlay's half of the machine: every point where the
// shared walk needs a meaning only the task or the box can give.
type overlayHooks[F ~int, K ~int] interface {
	// note re-writes the status row for the stage the shell just entered.
	note()
	// exit closes the overlay (esc or q on the menu).
	exit()
	// subject names the overlay's target the way its status rows do ("edit
	// t-…", "box e-…"); the rolling-back refusal is worded around it.
	subject() string
	// fieldCount is the number of menu rows; the menu cursor wraps over them.
	fieldCount() int
	// openField is ⏎ on a menu row.
	openField(F) tea.Cmd
	// listRows is the list stage's rows, recomputed on every key.
	listRows() []string
	// listSelect is ⏎ or x on the list row under the cursor.
	listSelect(rows []string) tea.Cmd
	// listKey is every other key in the list stage (the overlay's own adds,
	// deletes, rewords).
	listKey(msg tea.KeyPressMsg) tea.Cmd
	// gateKey is any key in the gate stage.
	gateKey(msg tea.KeyPressMsg) tea.Cmd
	// inputCancel is esc in the input, after the blur: the overlay decides
	// which stage was behind the input.
	inputCancel(K) tea.Cmd
	// inputCommit is ⏎ in the input, after the blur, with the text trimmed.
	// v may be empty — an empty ⏎ is a back-out the overlay words itself.
	inputCommit(K, string) tea.Cmd
}

// newOverlayInput is the text input both overlays type into, at the one width
// their boxes are laid out for.
func newOverlayInput() textinput.Model {
	ti := textinput.New()
	ti.SetWidth(48)
	return ti
}

// onKey is the walk. The caller has already answered ctrl+c and resolved the
// task or box, so a nil hooks target never reaches here.
func (s *overlayShell[F, K]) onKey(m *Model, msg tea.KeyPressMsg, h overlayHooks[F, K]) tea.Cmd {
	if s.stage == stageInput {
		return s.onInputKey(m, msg, h)
	}

	switch {
	case key.Matches(msg, m.keys.Cancel):
		if s.stage == stageMenu {
			h.exit()
		} else {
			s.stage, s.listIdx = stageMenu, 0
			h.note()
		}
		return nil
	case key.Matches(msg, m.keys.Quit) && s.stage == stageMenu:
		h.exit()
		return nil
	}

	switch s.stage {
	case stageMenu:
		switch {
		case key.Matches(msg, m.keys.Up):
			s.menuIdx = wrapIdx(s.menuIdx-1, h.fieldCount())
		case key.Matches(msg, m.keys.Down):
			s.menuIdx = wrapIdx(s.menuIdx+1, h.fieldCount())
		case key.Matches(msg, m.keys.Commit):
			return h.openField(F(s.menuIdx))
		}
	case stageGate:
		return h.gateKey(msg)
	case stageList:
		rows := h.listRows()
		switch {
		case key.Matches(msg, m.keys.Up):
			s.listIdx = wrapIdx(s.listIdx-1, len(rows))
		case key.Matches(msg, m.keys.Down):
			s.listIdx = wrapIdx(s.listIdx+1, len(rows))
		case key.Matches(msg, m.keys.Commit), key.Matches(msg, m.keys.Check):
			return h.listSelect(rows)
		default:
			return h.listKey(msg)
		}
	}
	return nil
}

func (s *overlayShell[F, K]) onInputKey(m *Model, msg tea.KeyPressMsg, h overlayHooks[F, K]) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Cancel):
		s.input.Blur()
		return h.inputCancel(s.inputFor)
	case key.Matches(msg, m.keys.Commit):
		v := strings.TrimSpace(s.input.Value())
		// Refused BEFORE the blur and the stage move in inputCommit. Every
		// write funnel refuses it too, but only after the modal has closed
		// over hand-typed text that nothing can recover; leaving the input
		// open and focused is what lets the user land it once the re-read
		// arrives. An empty ⏎ is a back-out, not a write, so it still passes.
		if v != "" && m.refuseWhileRollingBack(h.subject()) {
			m.status += "; ⏎ again in a moment"
			return s.input.Focus()
		}
		s.input.Blur()
		return h.inputCommit(s.inputFor, v)
	}
	var c tea.Cmd
	s.input, c = s.input.Update(msg)
	return c
}

// startInput moves into the input stage on kind, seeded with value.
func (s *overlayShell[F, K]) startInput(h overlayHooks[F, K], kind K, value, placeholder string) tea.Cmd {
	s.stage, s.inputFor = stageInput, kind
	s.input.SetValue(value)
	s.input.Placeholder = placeholder
	h.note()
	return s.input.Focus()
}

// wrapIdx is i modulo n for a cursor that wraps at both ends; an empty list
// parks it on 0.
func wrapIdx(i, n int) int {
	if n <= 0 {
		return 0
	}
	return ((i % n) + n) % n
}

// ---- rendering --------------------------------------------------------------

// overlayInner is the body width of both overlays' boxes.
func (m *Model) overlayInner() int {
	return clamp(m.w/3, 44, 72)
}

// overlayLayer boxes body under head in the peek's style and floats it a third
// of the way down the frame, above everything but the help overlay.
func (m *Model) overlayLayer(id, head, body string) *lg.Layer {
	th := m.th
	inner := m.overlayInner()
	box := th.peek.Render(pad(th.peekHdr.Render(head), inner) + "\n\n" + body)
	box = lg.NewStyle().MaxWidth(m.w).MaxHeight(m.h).Render(box)
	x := maxInt(0, (m.w-lg.Width(box))/2)
	y := maxInt(0, (m.h-lg.Height(box))/3)
	return lg.NewLayer(box).ID(id).X(x).Y(y).Z(zEdit)
}

// menuRow is one field row of an overlay menu: its name and its current value.
type menuRow struct{ name, cur string }

// renderOverlayMenu draws the field rows with the cursor on idx and the menu's
// key line under them. Every value is truncated to its cell, measured — the
// values can be CJK, and on the real board an epic title runs to 139 display
// cells and a goal line longer still.
func (m *Model) renderOverlayMenu(rows []menuRow, idx, inner int) string {
	th := m.th
	room := maxInt(4, inner-13)
	var b strings.Builder
	for i, r := range rows {
		cursor, style := "  ", th.base
		if i == idx {
			cursor, style = "▌ ", th.peekHdr
		}
		cur := r.cur
		if cur == "" {
			cur = "—"
		}
		b.WriteString(cursor + style.Render(pad(r.name, 10)) +
			th.muted.Render(pad(ansi.Truncate(cur, room, "…"), room)) + "\n")
	}
	b.WriteString("\n" + th.dim.Render(pad("↑↓ field · ⏎ edit · esc close · ^c quit", inner)))
	return b.String()
}

// renderOverlayInput draws the input stage: the input's title, the input, and
// its two keys.
func (m *Model) renderOverlayInput(title string, input textinput.Model, inner int) string {
	th := m.th
	return th.peekHdr.Render(title) + "\n\n" +
		input.View() + "\n" + th.dim.Render(pad("⏎ apply · esc back", inner))
}

// overlayListChrome is every line of the list stage that is NOT a row: the
// peek box's border (2), the overlay header and its blank line (2), the stage
// header and its blank line (2), and the blank line plus the key hints at the
// foot (2). The rows get whatever is left, so the hints are never the thing
// that falls off the bottom of the terminal.
const overlayListChrome = 8

// overlayListWindow picks the slice of rows to draw so the cursor stays on
// screen with context around it. budget is the whole allowance in LINES, hints
// included: a clipped head or tail spends one of them on its "N above" /
// "+N below" line, the same way a clipped column announces itself. marks is
// false when the rows are not clipped at all, and also when the budget is too
// small to afford a hint — the rows take every line rather than the footer
// losing its own.
//
// The window is derived from idx on every frame instead of being stored, so
// the frame stays a pure function of the state -dump already prints.
func overlayListWindow(n, idx, budget int) (start, end int, marks bool) {
	if budget < 1 {
		budget = 1
	}
	if n <= budget {
		return 0, n, false
	}
	if budget < 3 {
		start = clamp(idx-budget/2, 0, n-budget)
		return start, start + budget, false
	}
	// Worst case both hints show; the one that turns out unnecessary hands its
	// line back to the rows.
	w := budget - 2
	start = clamp(idx-w/2, 0, n-w)
	switch {
	case start == 0:
		return 0, budget - 1, true
	case start+w >= n:
		return n - (budget - 1), n, true
	}
	return start, start + w, true
}

// renderOverlayList draws the list stage: hdr, the rows windowed around idx and
// decorated by mark, and foot. mark receives the ABSOLUTE row index — only the
// render is windowed; listIdx walks, and wraps over, the whole row list, and
// the index mark sees is the one listSelect reads.
func (m *Model) renderOverlayList(hdr, foot string, rows []string, idx, inner, budget int, mark func(i int, row string) string) string {
	th := m.th
	var b strings.Builder
	b.WriteString(th.peekHdr.Render(hdr) + "\n\n")
	if len(rows) == 0 {
		b.WriteString(th.dim.Render(pad("(empty)", inner)) + "\n")
	}
	start, end, marks := overlayListWindow(len(rows), idx, budget)
	if marks && start > 0 {
		b.WriteString(th.dim.Render(pad(fmt.Sprintf("%d above", start), inner)) + "\n")
	}
	for i := start; i < end; i++ {
		cursor, style := "  ", th.base
		if i == idx {
			cursor, style = "▌ ", th.peekHdr
		}
		b.WriteString(cursor + style.Render(pad(ansi.Truncate(mark(i, rows[i]), inner-2, "…"), inner-2)) + "\n")
	}
	if marks && end < len(rows) {
		b.WriteString(th.dim.Render(pad(fmt.Sprintf("+%d below", len(rows)-end), inner)) + "\n")
	}
	b.WriteString("\n" + th.dim.Render(pad(foot, inner)))
	return b.String()
}

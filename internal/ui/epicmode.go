package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The epic overlay (t-6p45): everything `furrow epic add/set/activate/
// deactivate/dep` can write, from the slice panel's epic axis — the one surface
// that already lists boxes with the progress and stuck furrow derives.
//
// It looks like the task edit overlay and is deliberately NOT the same code: an
// epic is not a task. What it shares is the two-stage shape (menu → sub-editor,
// esc walks back out) and the list windowing.
//
// The write path is where they differ. Task edits are optimistic; epic writes
// are STORE-FIRST (board.Provider's epic family), because what they mean is
// furrow's: activate REFUSES a second box for a repo instead of stealing the
// slot, add invents the id, and progress/stuck/open_deps are all derived. So
// nothing here mutates the board — the overlay issues the write, the queue
// re-reads, and the row shows the new value when furrow's own truth arrives.
// Two consequences the overlay owes the user: it must not let an impatient
// second keypress queue a duplicate write, and it must say what it is waiting
// for.
//
// The lifecycle pair `epic done` / `epic reopen` is ONE row, not two, and it
// reads the box's own state to decide which verb it is. Two rows would put a
// dead one on screen at all times, and there is no third state to name.
//
// It is reachable at all because the read serves closed boxes: `done` leaves
// the box resolvable, so this overlay survives its own write and `reopen` is
// one keystroke back. Finding a box closed in an EARLIER session is the slice
// panel's job — its epic axis lists the open ones by default and takes `z` for
// the whole population.

type epicStage int

const (
	epicMenu    epicStage = iota
	epicList              // labels / repos / deps / meta cursor
	epicInput             // title / goal / new label / new repo / new dep / meta / activate reason
	epicConfirm           // one keystroke between the cursor and a lifecycle write
)

type epicField int

const (
	epicFieldTitle epicField = iota
	epicFieldGoal
	epicFieldActive
	epicFieldStanding
	epicFieldPinned
	epicFieldLabels
	epicFieldRepos
	epicFieldDeps
	epicFieldMeta
	// Last on purpose: it is the row whose write is a lifecycle decision, so
	// it must not sit where the cursor lands or where a mistyped ↓ reaches.
	epicFieldClosed
	epicFieldCount
)

func epicFieldName(f epicField) string {
	switch f {
	case epicFieldTitle:
		return "title"
	case epicFieldGoal:
		return "goal"
	case epicFieldActive:
		return "active"
	case epicFieldStanding:
		return "standing"
	case epicFieldPinned:
		return "pinned"
	case epicFieldLabels:
		return "labels"
	case epicFieldRepos:
		return "repos"
	case epicFieldDeps:
		return "deps"
	case epicFieldMeta:
		return "meta"
	case epicFieldClosed:
		return "closed"
	}
	return ""
}

// epicInputKind says what the text input commits to.
type epicInputKind int

const (
	epicInputTitle epicInputKind = iota
	epicInputGoal
	epicInputNewLabel
	epicInputNewRepo
	epicInputNewDep
	epicInputNewMeta
	epicInputReason
	epicInputNewBox
)

type epicState struct {
	// id is the box under edit, held for the overlay's whole life. Never the
	// panel's row INDEX: furrow orders `epic ls` active-first, so the very
	// write this overlay issues can move the row out from under the cursor.
	id       string
	stage    epicStage
	field    epicField
	menuIdx  int
	listIdx  int
	input    textinput.Model
	inputFor epicInputKind
	// creating is the new-box modal: there is no id yet, so the overlay is one
	// title input and nothing else until the store answers.
	creating bool
	// newRepo is the repo the new box will name, captured at open. Snapshotted
	// rather than re-derived at commit so the chip the user read and the write
	// they confirmed cannot disagree.
	newRepo string
}

// enterEpic opens the overlay on a box.
func (m *Model) enterEpic(id string) {
	if m.b.Epic(id) == nil {
		m.fail("%s is not a box on this board", id)
		return
	}
	m.epic = &epicState{id: id, stage: epicMenu, input: newEpicInput()}
	m.mode = modeEpic
	m.noteEpicStage()
}

// enterEpicNew opens the new-box modal. The repo comes from the filter
// context's single-valued repo:, exactly quick add's inheritance — and it
// matters more here than there: a box naming no repo cannot be activated at
// all. NOT from the slice: `A` is only answered on the epic axis, and the
// axis switch that got the panel there already cleared any repo-axis pick, so
// a slice-derived repo was a branch no key sequence could reach (found by
// review — the effective query is what survives the axis switch).
func (m *Model) enterEpicNew() tea.Cmd {
	_, _, repo, _ := inheritContext(m.effectiveQuery())
	m.epic = &epicState{stage: epicInput, inputFor: epicInputNewBox,
		creating: true, newRepo: repo, input: newEpicInput()}
	m.mode = modeEpic
	m.epic.input.Placeholder = "new box — its title"
	m.noteEpicStage()
	return m.epic.input.Focus()
}

func newEpicInput() textinput.Model {
	ti := textinput.New()
	ti.SetWidth(48)
	return ti
}

// reopenRefusedEpicAdd hands a refused new box's typed title back by reopening
// the modal seeded with it — the quick add's contract (reopenRefusedAdd,
// t-74y3), and it matters more here: the write is store-first, so nothing on
// the board even hints the title existed. Reopened only where the modal is at
// home — over the slice panel it was submitted from, or the bare board the
// panel was closed back to; anywhere else (another modal, the graph, the help
// overlay) the keyboard is already someone else's, and the failure note still
// names the title.
func (m *Model) reopenRefusedEpicAdd(op persistOp) tea.Cmd {
	if (m.mode != modeSlice && m.mode != modeNormal) || m.fullScreen() || m.fullHelp {
		return nil
	}
	m.epic = &epicState{stage: epicInput, inputFor: epicInputNewBox,
		creating: true, newRepo: op.epicAddRepo, input: newEpicInput()}
	m.mode = modeEpic
	m.epic.input.Placeholder = "new box — its title"
	m.epic.input.SetValue(op.epicAddTitle)
	// No noteEpicStage: onPersistDone's fail() runs after this returns and
	// owns the status row — the refusal must be what the user reads.
	return m.epic.input.Focus()
}

// exitEpic hands the keyboard back to the slice panel, not to the board: the
// panel is still open and still where the cursor is, and dropping to modeNormal
// would leave it rendered but unfocused.
func (m *Model) exitEpic() {
	m.epic = nil
	// …unless the panel is not on screen. A full-screen view renders no slice
	// panel, so handing the keyboard back to it there is the same invisible-
	// owner bug the overlay itself just had: the box overview opens this
	// overlay, and `s` before `E` leaves sliceOpen true underneath it.
	if m.sliceOpen && !m.fullScreen() {
		m.mode = modeSlice
		// The panel's note is the only place its keys are advertised, and the
		// overlay's own note ("box e-… — ⏎ pick a field · esc closes") is a FALSE
		// key claim once the panel has the keyboard back: there ⏎ slices.
		m.noteSliceAxis()
		return
	}
	m.mode = modeNormal
}

// epicBox resolves the box under edit, closing the overlay when a reload has
// dropped it rather than editing a ghost.
//
// Closing the box is NOT such a drop: the read serves closed boxes, so `Epic`
// still resolves one and the overlay stays up on its own `done`. That is what
// makes `reopen` reachable without hunting the box down again.
func (m *Model) epicBox() *board.EpicInfo {
	if m.epic == nil || m.epic.creating {
		return nil
	}
	// The id is captured BEFORE exitEpic, which nils m.epic — reading m.epic.id
	// after it is a nil dereference on the one path this branch exists for (a
	// reload, or another machine's sync, dropping the box mid-overlay).
	id := m.epic.id
	e := m.b.Epic(id)
	if e == nil {
		m.exitEpic()
		m.fail("%s left the board — the overlay closed", id)
	}
	return e
}

// noteEpicStage keeps the status row true as the overlay walks its stages. Same
// contract as the task overlay's: a stage change must never overwrite a refusal
// nobody has read yet.
func (m *Model) noteEpicStage() {
	e := m.epic
	if e == nil || m.statusErr {
		return
	}
	switch {
	case e.creating:
		m.note("new box — ⏎ creates · esc cancels")
	case e.stage == epicList:
		switch e.field {
		case epicFieldDeps:
			m.note("box %s · deps — ⏎/x remove · a add · esc back", e.id)
		case epicFieldMeta:
			m.note("box %s · meta — ⏎/x remove · a add k=v · esc back", e.id)
		default:
			m.note("box %s · %s — ⏎/x toggle · a add · esc back", e.id, epicFieldName(e.field))
		}
	case e.stage == epicInput:
		m.note("box %s · %s — ⏎ apply · esc back", e.id, epicInputTitleFor(e.inputFor))
	case e.stage == epicConfirm && e.field == epicFieldClosed:
		box := m.b.Epic(e.id)
		switch {
		case box == nil:
			m.note("box %s · closed — ⏎ confirms · esc backs out", e.id)
		case !box.Closed.IsZero():
			// furrow's own wording, not a paraphrase: reopening does NOT
			// re-activate, and a note that implied it would promise something
			// the next frame contradicts.
			m.note("reopen %s — it comes back OPEN and INACTIVE · ⏎ confirms · esc backs out", e.id)
		default:
			// furrow closes a box with open members at exit 0, so this line is
			// the ONLY thing standing between one keystroke and a box closed
			// over live work.
			left := box.Total - box.Done
			m.note("close %s — %d/%d done, %d still open · ⏎ confirms · esc backs out",
				e.id, box.Done, box.Total, left)
		}
	case e.stage == epicConfirm:
		m.note("box %s · %s — ⏎ confirms · esc backs out", e.id, epicFieldName(e.field))
	default:
		m.note("box %s — ⏎ pick a field · esc closes", e.id)
	}
}

func (m *Model) onEpicKey(msg tea.KeyPressMsg) tea.Cmd {
	// Checked before anything resolves the box: ctrl+c is the way out of every
	// modal, and in the input stages `q` types.
	if key.Matches(msg, m.keys.ForceQuit) {
		return m.quitOrFlush()
	}
	e := m.epic
	if e == nil {
		return nil
	}
	if e.creating {
		return m.onEpicNewKey(msg)
	}
	if m.epicBox() == nil {
		return nil
	}
	if e.stage == epicInput {
		return m.onEpicInputKey(msg)
	}

	switch {
	case key.Matches(msg, m.keys.Cancel):
		if e.stage == epicMenu {
			m.exitEpic()
		} else {
			e.stage, e.listIdx = epicMenu, 0
			m.noteEpicStage()
		}
		return nil
	case key.Matches(msg, m.keys.Quit) && e.stage == epicMenu:
		m.exitEpic()
		return nil
	}

	switch e.stage {
	case epicMenu:
		switch {
		case key.Matches(msg, m.keys.Up):
			e.menuIdx = (e.menuIdx + int(epicFieldCount) - 1) % int(epicFieldCount)
		case key.Matches(msg, m.keys.Down):
			e.menuIdx = (e.menuIdx + 1) % int(epicFieldCount)
		case key.Matches(msg, m.keys.Commit):
			return m.openEpicField(epicField(e.menuIdx))
		}
	case epicConfirm:
		if key.Matches(msg, m.keys.Commit) {
			return m.commitEpicConfirm()
		}
	case epicList:
		return m.onEpicListKey(msg)
	}
	return nil
}

// onEpicNewKey is the new-box modal: one input, and no board write until the
// store hands back an id.
func (m *Model) onEpicNewKey(msg tea.KeyPressMsg) tea.Cmd {
	e := m.epic
	switch {
	case key.Matches(msg, m.keys.Cancel):
		e.input.Blur()
		m.exitEpic()
		return nil
	case key.Matches(msg, m.keys.Commit):
		title := strings.TrimSpace(e.input.Value())
		if title == "" {
			m.fail("a box needs a title")
			return nil
		}
		if m.refuseWhileWriting("new box") {
			return nil
		}
		if m.rollingBack {
			// The store refused the last write and the board is rolling back;
			// every write path refuses until the re-read. Checked BEFORE the
			// modal closes — the queue's own refusal comes after exitEpic, so
			// reaching it would eat the typed title all over again, exactly on
			// the reopened-after-refusal retry (found by review; the quick
			// add's onAddKey keeps the same guard for the same reason).
			m.fail("the store refused the last write — rolling back; press ⏎ again in a moment")
			return nil
		}
		opts := board.EpicAddOptions{}
		if e.newRepo != "" {
			opts.Repos = []string{e.newRepo}
		}
		e.input.Blur()
		newRepo := e.newRepo
		m.exitEpic()
		// A pre-shaped op, not epicWrite: the typed title rides the op so a
		// store refusal can reopen this modal with it (reopenRefusedEpicAdd)
		// instead of eating it — the quick add's t-74y3 contract.
		prov := m.prov
		return m.epicWriteOp(persistOp{
			label: "epic add " + title, noLocal: true,
			epicAddTitle: title, epicAddRepo: newRepo,
			run: func() ([]string, error) {
				_, err := prov.EpicAdd(title, opts)
				return nil, err
			},
		})
	}
	var c tea.Cmd
	e.input, c = e.input.Update(msg)
	return c
}

func (m *Model) openEpicField(f epicField) tea.Cmd {
	e := m.epic
	box := m.b.Epic(e.id)
	if box == nil {
		return nil
	}
	e.field, e.listIdx = f, 0
	switch f {
	case epicFieldTitle:
		// No clearing form: `epic set --title ""` is exit 2, so the input
		// refuses an empty submission rather than queueing a doomed write.
		return m.startEpicInput(epicInputTitle, box.Title, "the box's title")
	case epicFieldGoal:
		return m.startEpicInput(epicInputGoal, box.Goal, "the closing condition · empty clears")
	case epicFieldActive:
		if box.Active {
			// Deactivating is not destructive but it does drop the box `furrow
			// next` scopes to, so it gets the same one-keystroke gate.
			e.stage = epicConfirm
			m.noteEpicStage()
			return nil
		}
		// Activating carries furrow's --reason, which is appended to the box's
		// own body as the activation record: the input IS the confirmation, and
		// it collects the thing that keeps the switch visible next session.
		return m.startEpicInput(epicInputReason, "", "who asked for this switch · empty omits it")
	case epicFieldStanding, epicFieldPinned, epicFieldClosed:
		e.stage = epicConfirm
		m.noteEpicStage()
		return nil
	case epicFieldLabels, epicFieldRepos, epicFieldDeps, epicFieldMeta:
		e.stage = epicList
		m.noteEpicStage()
	}
	return nil
}

func (m *Model) startEpicInput(kind epicInputKind, value, placeholder string) tea.Cmd {
	e := m.epic
	e.stage, e.inputFor = epicInput, kind
	e.input.SetValue(value)
	e.input.Placeholder = placeholder
	e.input.SetWidth(48)
	m.noteEpicStage()
	return e.input.Focus()
}

func (m *Model) onEpicListKey(msg tea.KeyPressMsg) tea.Cmd {
	e := m.epic
	box := m.b.Epic(e.id)
	if box == nil {
		return nil
	}
	rows := m.epicListRows(box)
	switch {
	case key.Matches(msg, m.keys.Up):
		if len(rows) > 0 {
			e.listIdx = (e.listIdx + len(rows) - 1) % len(rows)
		}
	case key.Matches(msg, m.keys.Down):
		if len(rows) > 0 {
			e.listIdx = (e.listIdx + 1) % len(rows)
		}
	case key.Matches(msg, m.keys.Commit), key.Matches(msg, m.keys.Check):
		return m.epicListSelect(box, rows)
	case msg.String() == "a":
		switch e.field {
		case epicFieldLabels:
			return m.startEpicInput(epicInputNewLabel, "", "new label")
		case epicFieldRepos:
			return m.startEpicInput(epicInputNewRepo, "", "owner/repo")
		case epicFieldDeps:
			return m.startEpicInput(epicInputNewDep, "", "e-… — the box this waits on")
		case epicFieldMeta:
			return m.startEpicInput(epicInputNewMeta, "", "key=value")
		}
	}
	return nil
}

// epicListSelect is ⏎/x on a list row.
func (m *Model) epicListSelect(box *board.EpicInfo, rows []string) tea.Cmd {
	e := m.epic
	if e.listIdx >= len(rows) {
		return nil
	}
	val := rows[e.listIdx]
	switch e.field {
	case epicFieldLabels:
		p := board.EpicPatch{AddLabels: []string{val}}
		if containsStrUI(box.Labels, val) {
			p = board.EpicPatch{RmLabels: []string{val}}
		}
		return m.epicPatch("label", p)
	case epicFieldRepos:
		p := board.EpicPatch{AddRepos: []string{val}}
		if containsStrUI(box.Repos, val) {
			p = board.EpicPatch{RmRepos: []string{val}}
		}
		return m.epicPatch("repo", p)
	case epicFieldDeps:
		// Every row IS an edge, so the toggle points one way: selecting
		// removes. Re-adding is `a`, because the vocabulary of potential deps
		// is every other box.
		dep := val
		id := e.id
		return m.epicWrite("epic dep rm "+id, func(p board.Provider) error {
			return p.EpicDepRm(id, dep)
		})
	case epicFieldMeta:
		return m.epicPatch("meta rm", board.EpicPatch{RmMeta: []string{metaKeyOf(val)}})
	}
	return nil
}

// No cursor clamp here: the rows only shrink when the store's re-read lands
// (~150ms later — until then the overlay still renders the old list), so
// recompute is where the cursor is pulled back in. Clamping at the gesture
// moved it even when the write was refused.

func (m *Model) onEpicInputKey(msg tea.KeyPressMsg) tea.Cmd {
	e := m.epic
	switch {
	case key.Matches(msg, m.keys.Cancel):
		e.input.Blur()
		switch e.inputFor {
		case epicInputTitle, epicInputGoal, epicInputReason:
			e.stage = epicMenu
		default:
			e.stage = epicList
		}
		m.noteEpicStage()
		return nil
	case key.Matches(msg, m.keys.Commit):
		v := strings.TrimSpace(e.input.Value())
		e.input.Blur()
		id := e.id
		switch e.inputFor {
		case epicInputTitle:
			e.stage = epicMenu
			if v == "" {
				// furrow answers exit 2 on an empty rename; refusing here keeps
				// the queue clean and the message specific.
				m.fail("a box title cannot be empty")
				return nil
			}
			return m.epicPatch("retitle", board.EpicPatch{Title: &v})
		case epicInputGoal:
			e.stage = epicMenu
			return m.epicPatch("goal", board.EpicPatch{Goal: &v})
		case epicInputReason:
			e.stage = epicMenu
			return m.epicWrite("epic activate "+id, func(p board.Provider) error {
				return p.EpicActivate(id, v)
			})
		case epicInputNewLabel:
			e.stage = epicList
			if v == "" {
				m.noteEpicStage()
				return nil
			}
			return m.epicPatch("label", board.EpicPatch{AddLabels: []string{v}})
		case epicInputNewRepo:
			e.stage = epicList
			if v == "" {
				m.noteEpicStage()
				return nil
			}
			return m.epicPatch("repo", board.EpicPatch{AddRepos: []string{v}})
		case epicInputNewDep:
			e.stage = epicList
			if v == "" {
				m.noteEpicStage()
				return nil
			}
			// No re-note after this: every write funnel writes the status row
			// itself (the pending write, or its refusal), so the input's
			// "⏎ apply" is already gone — and re-noting here erased the
			// "waiting for furrow" the store-first write had just put there.
			return m.epicWrite("epic dep "+id, func(p board.Provider) error {
				return p.EpicDepAdd(id, v)
			})
		case epicInputNewMeta:
			e.stage = epicList
			if v == "" {
				m.noteEpicStage()
				return nil
			}
			k, val, ok := strings.Cut(v, "=")
			k = strings.TrimSpace(k)
			if !ok || k == "" {
				m.fail("meta wants key=value, got %q", v)
				return nil
			}
			return m.epicPatch("meta", board.EpicPatch{
				SetMeta: map[string]string{k: strings.TrimSpace(val)}})
		}
		return nil
	}
	var c tea.Cmd
	e.input, c = e.input.Update(msg)
	return c
}

// commitEpicConfirm is ⏎ on the confirm stage: the lifecycle and declaration
// writes that are a single toggle with no text to collect.
func (m *Model) commitEpicConfirm() tea.Cmd {
	e := m.epic
	box := m.b.Epic(e.id)
	if box == nil {
		return nil
	}
	e.stage = epicMenu
	id := e.id
	switch e.field {
	case epicFieldActive:
		// furrow's previous-active suggestion travels back through the op's own
		// note pointer, never a model field: this closure runs on the queue's
		// goroutine while the UI thread renders.
		suggestion := new(string)
		return m.epicWriteNoting("epic deactivate "+id, suggestion, func(p board.Provider) error {
			prev, err := p.EpicDeactivate(id)
			if err == nil && prev.ID != "" {
				*suggestion = "previous: " + prev.ID + " " + prev.Title
			}
			return err
		})
	case epicFieldStanding:
		on := !box.Standing
		return m.epicPatch("standing", board.EpicPatch{Standing: &on})
	case epicFieldPinned:
		on := !box.Pinned
		return m.epicPatch("pinned", board.EpicPatch{Pinned: &on})
	case epicFieldClosed:
		if !box.Closed.IsZero() {
			return m.epicWrite("epic reopen "+id, func(p board.Provider) error {
				return p.EpicReopen(id)
			})
		}
		// Same suggestion pointer `deactivate` uses — but only when this box
		// actually held the slot. furrow answers `previous` either way
		// (measured: closing an INACTIVE box still names one), and "where to
		// return" for a slot nobody gave up is an instruction about nothing.
		wasActive := box.Active
		suggestion := new(string)
		return m.epicWriteNoting("epic done "+id, suggestion, func(p board.Provider) error {
			prev, err := p.EpicDone(id)
			if err == nil && wasActive && prev.ID != "" {
				*suggestion = "previous: " + prev.ID + " " + prev.Title
			}
			return err
		})
	}
	return nil
}

// epicPatch funnels every `epic set`-shaped write.
func (m *Model) epicPatch(label string, p board.EpicPatch) tea.Cmd {
	if p.Empty() {
		// furrow refuses a set with no change flag (exit 2), and a write that
		// can only fail should never reach the queue.
		m.fail("nothing to change")
		return nil
	}
	id := m.epic.id
	return m.epicWrite(label+" "+id, func(prov board.Provider) error {
		return prov.EpicSet(id, p)
	})
}

// epicWrite queues one store-first epic write and says what it is waiting for.
// Nothing is applied locally, so until the write lands and the queue re-reads,
// the overlay is still showing the OLD value — which is exactly why the note
// has to name the pending write rather than claim it happened.
func (m *Model) epicWrite(label string, run func(board.Provider) error) tea.Cmd {
	return m.epicWriteNoting(label, nil, run)
}

// epicWriteNoting is epicWrite with a place for prose the write itself computes
// (see persistOp.note).
func (m *Model) epicWriteNoting(label string, note *string, run func(board.Provider) error) tea.Cmd {
	prov := m.prov
	return m.epicWriteOp(persistOp{
		label: label, noLocal: true, note: note,
		run: func() ([]string, error) { return nil, run(prov) },
	})
}

// epicWriteOp queues one pre-shaped store-first epic op and says what it is
// waiting for.
func (m *Model) epicWriteOp(op persistOp) tea.Cmd {
	if m.refuseWhileWriting(op.label) {
		return nil
	}
	queued := len(m.pending)
	cmd := m.enqueueStoreFirstOp(op)
	if len(m.pending) == queued {
		// Refused inside the rollback window, which already said why. A nil cmd
		// alone does NOT mean refused — an epic write queued behind an
		// in-flight task write also returns nil — so the queue length is what
		// distinguishes them, and noting unconditionally would erase the
		// refusal the user has not read yet.
		return nil
	}
	m.note("%s — waiting for furrow", op.label)
	return cmd
}

// refuseWhileWriting reports (and explains) that a second store-first gesture
// must wait.
//
// A store-first write changes nothing on screen until it lands, so pressing
// again is the natural mistake — and the queue would happily carry both. The
// harm is not that furrow refuses the repeat (measured on v4.0.0: re-activating
// the same box is exit 0, changed:[], and does not even add a second activation
// record). It is that the overlay's rows still show the PRE-write values, so the
// second gesture is aimed at a board the user cannot yet see, and its report
// lands on the status line over the first write's.
func (m *Model) refuseWhileWriting(label string) bool {
	switch {
	case m.storeFirstInflight():
		m.fail("%s — a box write is still in flight; the board re-reads when it lands", label)
	case m.storeFirstUnread:
		// The window AFTER the write landed and BEFORE its re-read arrives. The
		// queue is empty, so the in-flight check above passes — but the rows are
		// still the pre-write ones, so a toggle here recomputes from a stale
		// value and a dep removal addresses an edge furrow has already dropped
		// ("X is not a dependency of Y").
		m.fail("%s — the last box write landed; waiting for the board to re-read it", label)
	default:
		return false
	}
	return true
}

// epicListRows is the current list stage's rows, as raw values.
func (m *Model) epicListRows(box *board.EpicInfo) []string {
	switch m.epic.field {
	case epicFieldLabels:
		return vocabUnion(m.labelVocab(), box.Labels)
	case epicFieldRepos:
		return vocabUnion(m.repoVocab(), box.Repos)
	case epicFieldDeps:
		return append([]string(nil), box.Deps...)
	case epicFieldMeta:
		rows := make([]string, 0, len(box.Meta))
		for _, k := range box.MetaKeys() {
			rows = append(rows, k+"="+box.Meta[k])
		}
		return rows
	}
	return nil
}

func metaKeyOf(row string) string {
	k, _, _ := strings.Cut(row, "=")
	return k
}

// ---- rendering --------------------------------------------------------------

func epicInputTitleFor(k epicInputKind) string {
	switch k {
	case epicInputTitle:
		return "rename the box"
	case epicInputGoal:
		return "goal — the closing condition"
	case epicInputNewLabel:
		return "add label"
	case epicInputNewRepo:
		return "attach repo"
	case epicInputNewDep:
		return "wait on a box"
	case epicInputNewMeta:
		return "set meta"
	case epicInputReason:
		return "activate — who asked, and why"
	case epicInputNewBox:
		return "new box"
	}
	return ""
}

// epicClosedCell names the state, not the verb: the row's cell answers "is
// this box closed", and the confirm stage names what ⏎ would do about it.
func epicClosedCell(box *board.EpicInfo) string {
	if box.Closed.IsZero() {
		return "no — open"
	}
	return "yes — " + box.Closed.Local().Format("2006-01-02")
}

// epicActiveCell is the `active` row's value AND its precondition. furrow
// refuses a second active box for a repo rather than stealing the slot, and
// refuses a repo-less box outright — so the row says which of those applies
// BEFORE the keystroke, instead of letting furrow's exit 2 be the first news.
// It reads the served Active/Repos fields; the rule itself stays furrow's.
func (m *Model) epicActiveCell(box *board.EpicInfo) string {
	if box.Active {
		return "yes"
	}
	// The closed check leads because it outranks the others: furrow answers
	// "epic X is closed — run `furrow epic reopen X` first" (measured on
	// v5.0.0) before it looks at repos or the slot at all, so naming a repo
	// problem here would send the reader to fix the wrong thing.
	if !box.Closed.IsZero() {
		return "no — closed; reopen it first"
	}
	if len(box.Repos) == 0 {
		return "no — attach a repo first"
	}
	if holder := m.b.ActiveHolder(box.ID); holder != "" {
		return "no — slot held by " + holder
	}
	return "no"
}

func yesNo(b bool) string {
	if b {
		return "yes"
	}
	return "no"
}

// epicLayer draws the overlay, in the same box style as the task edit overlay.
func (m *Model) epicLayer() *lg.Layer {
	th := m.th
	e := m.epic
	if e == nil {
		return nil
	}
	inner := clamp(m.w/3, 44, 72)

	var head, body string
	switch {
	case e.creating:
		head = "new box"
		inherit := th.dim.Render("no repo — attach one before activating")
		if e.newRepo != "" {
			inherit = th.chipAlt.Render(" repo " + e.newRepo + " ")
		}
		body = th.peekHdr.Render("title") + "\n\n" + e.input.View() + "\n" +
			pad(inherit, inner) + "\n" +
			th.dim.Render(pad("⏎ creates · esc cancels", inner))
	default:
		box := m.b.Epic(e.id)
		if box == nil {
			return nil
		}
		head = "box " + box.ID
		switch e.stage {
		case epicMenu:
			body = m.renderEpicMenu(box, inner)
		case epicInput:
			body = th.peekHdr.Render(epicInputTitleFor(e.inputFor)) + "\n\n" +
				e.input.View() + "\n" + th.dim.Render(pad("⏎ apply · esc back", inner))
		case epicConfirm:
			body = m.renderEpicConfirm(box, inner)
		case epicList:
			body = m.renderEpicList(box, inner, maxInt(1, m.h-editListChrome))
		}
	}

	panel := th.peek.Render(pad(th.peekHdr.Render(head), inner) + "\n\n" + body)
	panel = lg.NewStyle().MaxWidth(m.w).MaxHeight(m.h).Render(panel)
	x := maxInt(0, (m.w-lg.Width(panel))/2)
	y := maxInt(0, (m.h-lg.Height(panel))/3)
	return lg.NewLayer(panel).ID("epic").X(x).Y(y).Z(zEdit)
}

func (m *Model) renderEpicMenu(box *board.EpicInfo, inner int) string {
	th := m.th
	depCell := "—"
	if n := len(box.Deps); n > 0 {
		depCell = strings.Join(box.Deps, ",")
		if open := len(box.OpenDeps); open > 0 {
			depCell = fmt.Sprintf("→%d of %d", open, n)
		}
	}
	metaCell := "—"
	if keys := box.MetaKeys(); len(keys) > 0 {
		metaCell = strings.Join(keys, ",")
	}
	rows := []struct{ name, cur string }{
		{epicFieldName(epicFieldTitle), box.Title},
		{epicFieldName(epicFieldGoal), box.Goal},
		{epicFieldName(epicFieldActive), m.epicActiveCell(box)},
		{epicFieldName(epicFieldStanding), yesNo(box.Standing)},
		{epicFieldName(epicFieldPinned), yesNo(box.Pinned)},
		{epicFieldName(epicFieldLabels), strings.Join(box.Labels, ",")},
		{epicFieldName(epicFieldRepos), strings.Join(box.Repos, ",")},
		{epicFieldName(epicFieldDeps), depCell},
		{epicFieldName(epicFieldMeta), metaCell},
		{epicFieldName(epicFieldClosed), epicClosedCell(box)},
	}

	var b strings.Builder
	// The derived line first: progress and stuck are furrow's verdict on this
	// box and nothing in the menu can change them, so they belong above the
	// editable rows rather than pretending to be one.
	derived := fmt.Sprintf("%d/%d done", box.Done, box.Total)
	if box.Stuck {
		derived += " · " + th.warn.Render("STUCK")
	}
	b.WriteString(th.muted.Render(pad(derived, inner)) + "\n\n")
	for i, r := range rows {
		cursor, style := "  ", th.base
		if i == m.epic.menuIdx {
			cursor, style = "▌ ", th.peekHdr
		}
		cur := r.cur
		if cur == "" {
			cur = "—"
		}
		// Measured, never a hard-coded cell budget: every one of these values
		// can be CJK, and the real board's epic titles run to 139 display cells
		// (111 boxes, median 8, p90 37) — the goal line is longer still.
		room := maxInt(4, inner-13)
		b.WriteString(cursor + style.Render(pad(r.name, 10)) +
			th.muted.Render(pad(ansi.Truncate(cur, room, "…"), room)) + "\n")
	}
	b.WriteString("\n" + th.dim.Render(pad("↑↓ field · ⏎ edit · esc close · ^c quit", inner)))
	return b.String()
}

func (m *Model) renderEpicConfirm(box *board.EpicInfo, inner int) string {
	th := m.th
	var hdr, detail string
	switch m.epic.field {
	case epicFieldActive:
		hdr = "deactivate this box"
		detail = "`furrow next` stops scoping to it. The box stays open."
	case epicFieldStanding:
		hdr = "standing → " + yesNo(!box.Standing)
		detail = "A standing box is exempt from the finish-shaped nags."
	case epicFieldPinned:
		hdr = "pinned → " + yesNo(!box.Pinned)
		detail = "A pinned box's actionable tasks lead next/brief regardless of scope."
	case epicFieldClosed:
		if !box.Closed.IsZero() {
			hdr = "reopen this box"
			detail = "It comes back OPEN and INACTIVE. furrow never chains reopening to activating, so choosing it again is a separate act."
			break
		}
		hdr = "close this box"
		// furrow closes a box with open members at exit 0 (measured on
		// v5.0.0), so this count is the entire warning that exists. Saying the
		// way back in the same breath is what keeps the gate a gate rather
		// than a scare.
		detail = fmt.Sprintf("%d/%d done — %d still open under it. furrow closes it anyway. The way back is this same row, which reads `reopen` once it is closed.",
			box.Done, box.Total, box.Total-box.Done)
		if box.Active {
			detail += " This is the ACTIVE box: closing vacates its repo slot too."
		}
	}
	var b strings.Builder
	b.WriteString(th.peekHdr.Render(hdr) + "\n\n")
	for _, l := range wrapLines(box.Title, inner) {
		b.WriteString(th.base.Render(pad(l, inner)) + "\n")
	}
	b.WriteString("\n")
	for _, l := range wrapLines(detail, inner) {
		b.WriteString(th.muted.Render(pad(l, inner)) + "\n")
	}
	b.WriteString("\n" + th.dim.Render(pad("⏎ confirms · esc backs out", inner)))
	return b.String()
}

func (m *Model) renderEpicList(box *board.EpicInfo, inner, budget int) string {
	th := m.th
	e := m.epic
	rows := m.epicListRows(box)

	hdr, foot := epicFieldName(e.field), "⏎/x toggle · a add · esc back"
	mark := func(_ int, row string) string { return row }
	switch e.field {
	case epicFieldLabels:
		foot = "⏎/x toggle · a new label · esc back"
		mark = func(_ int, row string) string {
			if containsStrUI(box.Labels, row) {
				return "[x] " + row
			}
			return "[ ] " + row
		}
	case epicFieldRepos:
		hdr = "repos — a box with none cannot be activated"
		foot = "⏎/x attach/detach · a new repo · esc back"
		mark = func(_ int, row string) string {
			if containsStrUI(box.Repos, row) {
				return "[x] " + row
			}
			return "[ ] " + row
		}
	case epicFieldDeps:
		hdr = "deps — open this box after those close"
		foot = "⏎/x remove · a add · esc back"
		open := make(map[string]bool, len(box.OpenDeps))
		for _, d := range box.OpenDeps {
			open[d] = true
		}
		mark = func(_ int, row string) string {
			// The peek's wording, not a second vocabulary — see the block
			// above `epic waits on` in peek.go for why each word is the one
			// furrow itself uses.
			de := m.b.Epic(row)
			label := row
			if de != nil {
				label = fmt.Sprintf("%s (%d/%d) %s", row, de.Done, de.Total, de.Title)
			}
			if !open[row] {
				switch {
				case de == nil:
					return glyphDone + " " + label + " (missing)"
				case !de.Closed.IsZero():
					return glyphDone + " " + label + " (closed)"
				default:
					return glyphDone + " " + label + " (satisfied)"
				}
			}
			return glyphOpen + " " + label
		}
	case epicFieldMeta:
		hdr = "meta — furrow stores it and never reads it"
		foot = "⏎/x remove · a add k=v · esc back"
	}

	var b strings.Builder
	b.WriteString(th.peekHdr.Render(hdr) + "\n\n")
	if len(rows) == 0 {
		b.WriteString(th.dim.Render(pad("(empty)", inner)) + "\n")
	}
	start, end, marks := editListWindow(len(rows), e.listIdx, budget)
	if marks && start > 0 {
		b.WriteString(th.dim.Render(pad(fmt.Sprintf("%d above", start), inner)) + "\n")
	}
	for i := start; i < end; i++ {
		cursor, style := "  ", th.base
		if i == e.listIdx {
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

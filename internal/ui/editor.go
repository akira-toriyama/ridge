package ui

import (
	"github.com/akira-toriyama/ridge/internal/board"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"
)

type editorDoneMsg struct {
	id   string
	body string
	err  error
}

// applyEditorBody lands a $EDITOR result: the optimistic local apply plus
// the queued store write. Split out so a body held through the rollback
// window (model.go: heldBody) replays through the same path. The id names a
// task or a box (furrow's edit takes either); the box's half is SetEpicBody.
func (m *Model) applyEditorBody(msg editorDoneMsg) tea.Cmd {
	set := m.b.SetBody
	if m.b.Epic(msg.id) != nil {
		set = m.b.SetEpicBody
	}
	if err := set(msg.id, msg.body); err != nil {
		// The refusal is terminal for a flush the way a failed write is
		// (quitOrFlush cancels on those): a quit armed on THIS body as the
		// held write must not stay armed once the refusal removes it from
		// the drain — the queue is empty, nothing is left to fire tea.Quit,
		// and the armed flag would turn the next unrelated write into a
		// surprise exit.
		m.quitting = false
		m.fail("%v", err)
		return nil
	}
	m.recompute()
	m.note("%s body updated", msg.id)
	id, body := msg.id, msg.body
	return m.enqueuePersist("body "+id, func() ([]string, error) {
		return nil, m.prov.PersistBody(id, body)
	})
}

// editCmd suspends the TUI for $EDITOR on a task, the way furrow's `edit`
// does.
func (m *Model) editCmd(t *board.Task) tea.Cmd { return m.editBodyCmd(t.ID, t.Body) }

// editBodyCmd is editCmd over any body — a task's or a box's (the epic
// overlay's body stage); the result lands through applyEditorBody either way.
func (m *Model) editBodyCmd(id, body string) tea.Cmd {
	f, err := os.CreateTemp("", "furrow-poc-"+id+"-*.md")
	if err != nil {
		return func() tea.Msg { return editorDoneMsg{err: err} }
	}
	path := f.Name()
	if _, err := f.WriteString(body); err != nil {
		_ = f.Close()
		return func() tea.Msg { return editorDoneMsg{err: err} }
	}
	// A close failure here can mean an unflushed body — the editor would open
	// a truncated file and a save would feed the truncation back, so it is an
	// abort, not a shrug.
	if err := f.Close(); err != nil {
		return func() tea.Msg { return editorDoneMsg{err: err} }
	}

	ed := os.Getenv("EDITOR")
	if ed == "" {
		ed = "vi"
	}
	return tea.ExecProcess(exec.Command(ed, path), func(runErr error) tea.Msg { //nolint:gosec // G204: launching $EDITOR on our own temp file IS the feature
		defer func() { _ = os.Remove(path) }()
		if runErr != nil {
			return editorDoneMsg{id: id, err: runErr}
		}
		b, err := os.ReadFile(path) //nolint:gosec // path is the CreateTemp file made above
		return editorDoneMsg{id: id, body: string(b), err: err}
	})
}

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
// window (model.go: heldBody) replays through the same path.
func (m *Model) applyEditorBody(msg editorDoneMsg) tea.Cmd {
	if err := m.b.SetBody(msg.id, msg.body); err != nil {
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

// editCmd suspends the TUI for $EDITOR, the way furrow's `edit` does.
func (m *Model) editCmd(t *board.Task) tea.Cmd {
	f, err := os.CreateTemp("", "furrow-poc-"+t.ID+"-*.md")
	if err != nil {
		return func() tea.Msg { return editorDoneMsg{err: err} }
	}
	path := f.Name()
	if _, err := f.WriteString(t.Body); err != nil {
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
	id := t.ID
	// Launching $EDITOR on our own temp file IS the feature (G204/G304).
	return tea.ExecProcess(exec.Command(ed, path), func(runErr error) tea.Msg { //nolint:gosec
		defer func() { _ = os.Remove(path) }()
		if runErr != nil {
			return editorDoneMsg{id: id, err: runErr}
		}
		b, err := os.ReadFile(path) //nolint:gosec // path is the CreateTemp file made above
		return editorDoneMsg{id: id, body: string(b), err: err}
	})
}

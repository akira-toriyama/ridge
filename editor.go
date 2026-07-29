package main

import (
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"
)

type editorDoneMsg struct {
	id   string
	body string
	err  error
}

// editCmd suspends the TUI for $EDITOR, the way furrow's `edit` does.
func (m *Model) editCmd(t *Task) tea.Cmd {
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
	return tea.ExecProcess(exec.Command(ed, path), func(runErr error) tea.Msg {
		defer func() { _ = os.Remove(path) }()
		if runErr != nil {
			return editorDoneMsg{id: id, err: runErr}
		}
		b, err := os.ReadFile(path)
		return editorDoneMsg{id: id, body: string(b), err: err}
	})
}

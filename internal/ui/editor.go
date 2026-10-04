package ui

import (
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
)

// editorDoneMsg is a $EDITOR exit, the buffer read back. A stale result is
// one the record moved under (editorResult): body is not applied, and kept
// names the file the typed text was left in. same is a buffer the editor
// left as it was handed: nothing is applied or written.
type editorDoneMsg struct {
	id    string
	body  string
	err   error
	stale bool
	kept  string
	same  bool
}

// applyEditorBody lands a $EDITOR result: the optimistic local apply plus
// the queued store write. Split out so a body held through the rollback
// window (model.go: heldBody) replays through the same path. The id names a
// task or a box (furrow's edit takes either); the box's half is SetEpicBody.
func (m *Model) applyEditorBody(msg editorDoneMsg) tea.Cmd {
	if msg.same {
		// `furrow edit --body` stamps `updated` even for an identical body
		// (measured 2026-10-04), so the write this skips took a stale task
		// off `furrow revisit` for an editor that was only opened (t-zq7m).
		// The re-read is still owed on a live store: the editor showed the
		// store's record, which may be newer than the board's or gone, and
		// the stale fence that would have re-read was never reached.
		if !m.statusErr {
			m.note("%s body unchanged — nothing written", msg.id)
		}
		if m.prov.Live() {
			return m.reloadCmd("")
		}
		return nil
	}
	if msg.stale {
		// Terminal for a flush the way the refusal below is. The re-read is
		// owed now, as after any refusal that says the board is not the
		// store's: the record moved, and the stage under the user shows the
		// text the editor was handed.
		m.quitting = false
		m.fail("%s: the record changed while the editor was open — nothing written; your text is kept at %s (the board re-reads; e again to edit the moved record)", msg.id, msg.kept)
		return m.reloadCmd("")
	}
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
func (m *Model) editCmd(t *board.Task) tea.Cmd { return m.editBodyCmd(t.ID) }

// editBodyCmd is editCmd over any record — a task's or a box's (the epic
// overlay's body stage); the result lands through applyEditorBody either
// way. The text handed to $EDITOR is the store's NOW (Provider.ReadBody),
// not the loaded snapshot: a `furrow note` from another session between the
// load and this key was otherwise replaced wholesale by the save, with no
// word said (t-2wa3). The same read fences the save (editorResult). While
// a write is in flight no read is fired — the board's rule for `r` and the
// rm gate's: ridge's own queued note or body write leaves the file behind
// the screen until it lands, so the editor opened without the paragraph
// just typed and the save was then refused as stale (review finding, on
// the real board: an `a` followed by `e` inside the write's ~80ms).
func (m *Model) editBodyCmd(id string) tea.Cmd {
	if m.queueBusy() {
		m.fail("%s: a write is still in flight — let it land, then e again", id)
		return nil
	}
	prov := m.prov
	base, err := prov.ReadBody(id)
	if err != nil {
		m.fail("%s: %v", id, err)
		return nil
	}
	f, err := os.CreateTemp("", "furrow-poc-"+id+"-*.md")
	if err != nil {
		return func() tea.Msg { return editorDoneMsg{err: err} }
	}
	path := f.Name()
	if _, err := f.WriteString(base); err != nil {
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
		return editorResult(prov, id, base, path, runErr)
	})
}

// editorResult is the $EDITOR exit: the buffer read back, fenced against a
// record that moved while the editor held it — the store's record no longer
// reads as base, or is gone. The temp file is removed except on that
// refusal, where it is the only copy of the typed text. An untouched buffer
// is answered before the fence: with nothing typed there is nothing a moved
// record could lose. Runs on the process callback, off the UI thread: prov
// is the Provider, never the model.
func editorResult(prov board.Provider, id, base, path string, runErr error) editorDoneMsg {
	if runErr != nil {
		_ = os.Remove(path)
		return editorDoneMsg{id: id, err: runErr}
	}
	b, err := os.ReadFile(path) //nolint:gosec // path is the CreateTemp file made above
	if err != nil {
		_ = os.Remove(path)
		return editorDoneMsg{id: id, err: err}
	}
	if string(b) == base {
		_ = os.Remove(path)
		return editorDoneMsg{id: id, same: true}
	}
	if cur, err := prov.ReadBody(id); err != nil || cur != base {
		return editorDoneMsg{id: id, body: string(b), stale: true, kept: path}
	}
	_ = os.Remove(path)
	return editorDoneMsg{id: id, body: string(b)}
}

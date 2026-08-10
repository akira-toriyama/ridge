package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/akira-toriyama/ridge/internal/board"
)

// Quick add (t-m89b): GitHub Projects' "+ Add item". One text input, one
// Enter — and the ACTIVE FILTER'S metadata is inherited by the new task, the
// GH rule that makes adding from a filtered view mean "add into what I am
// looking at". Inherited values are shown as chips in the modal, never
// applied silently. Details (value/effort/due/…) are the edit overlay's job;
// the modal stays one line.

type addState struct {
	input textinput.Model
	opts  board.AddOptions
}

// A quick add committed while the store already had a writer (a queued
// persist, another add, a rollback re-read): it waits for the drain instead
// of racing — measured concurrently, furrow lost 15/20 racing writes and
// shed store shards (t-74y3).
type deferredAdd struct {
	title string
	opts  board.AddOptions
}

// addDoneMsg reports the store's answer: the new id, or the refusal — which
// carries the submission so a refusal can reopen the modal instead of eating
// the typed title.
type addDoneMsg struct {
	id    string
	err   error
	title string
	opts  board.AddOptions
}

// enterAdd opens the quick-add modal aimed at the focused lane (board) or
// the store's default lane (table — GH's bottom-of-table add).
func (m *Model) enterAdd() tea.Cmd {
	m.cancelDrag()
	ti := textinput.New()
	ti.Prompt = "+ "
	ti.Placeholder = "task title"
	ti.SetWidth(56)
	o := board.AddOptions{}
	if m.view == viewBoard {
		o.Lane = m.curLaneName()
	}
	o.Label, o.Epic, o.Repo = inheritContext(m.qRaw)
	m.add = &addState{input: ti, opts: o}
	m.mode = modeAdd
	return m.add.input.Focus()
}

// inheritContext lifts the single-valued, un-negated label:/epic:/repo:
// tokens out of the active query — exactly the values GH would stamp onto an
// item added under that filter. Comma'd (OR) and negated tokens inherit
// nothing: "one of these" is not a value to stamp.
func inheritContext(raw string) (label, epic, repo string) {
	for _, tok := range strings.Fields(raw) {
		k, v, ok := strings.Cut(tok, ":")
		if !ok || v == "" || strings.HasPrefix(k, "-") || strings.Contains(v, ",") {
			continue
		}
		switch strings.ToLower(k) {
		case "label":
			label = v
		case "epic":
			epic = v
		case "repo":
			repo = v
		}
	}
	return label, epic, repo
}

func (m *Model) onAddKey(msg tea.KeyPressMsg) tea.Cmd {
	a := m.add
	switch {
	case key.Matches(msg, m.keys.Cancel):
		m.mode, m.add = modeNormal, nil
		return nil
	case key.Matches(msg, m.keys.Commit):
		title := strings.TrimSpace(a.input.Value())
		if title == "" {
			m.fail("a title cannot be empty")
			return nil
		}
		m.mode, m.add = modeNormal, nil
		if m.inflight || len(m.pending) > 0 || m.addInFlight || m.rollingBack || len(m.deferredAdds) > 0 {
			// One store writer at a time (persist.go's invariant — quick add
			// used to bypass it and race the queue): wait for the drain.
			m.deferredAdds = append(m.deferredAdds, deferredAdd{title: title, opts: a.opts})
			m.note("adding… (queued)")
			return nil
		}
		return m.runAdd(title, a.opts)
	}
	var c tea.Cmd
	a.input, c = a.input.Update(msg)
	return c
}

// runAdd fires one add against the store, marking it as the single writer.
func (m *Model) runAdd(title string, opts board.AddOptions) tea.Cmd {
	m.addInFlight = true
	m.note("adding…")
	prov := m.prov
	return func() tea.Msg {
		id, err := prov.Add(title, opts)
		return addDoneMsg{id: id, err: err, title: title, opts: opts}
	}
}

// fireAdd pops the oldest deferred add — called at the queue's drain points.
func (m *Model) fireAdd() tea.Cmd {
	d := m.deferredAdds[0]
	m.deferredAdds = m.deferredAdds[1:]
	return m.runAdd(d.title, d.opts)
}

// onAddDone lands the store's answer: remember the id so the cursor moves
// onto the new card once the re-read delivers it. A refusal reopens the
// modal with the typed title — the store saying no must not eat the text.
func (m *Model) onAddDone(msg addDoneMsg) tea.Cmd {
	m.addInFlight = false
	if msg.err != nil {
		m.quitting = false // never exit on top of a swallowed refusal
		if m.mode != modeNormal {
			// The refusal lands ~100ms after the gesture; the user may
			// already be typing a SECOND add, lifting a card, or filtering.
			// Reopening the modal here would steal that mode and clobber
			// the newer draft — keep the title recoverable in the note
			// instead.
			m.fail("add %q: %v", msg.title, msg.err)
			return m.drainAfterAdd()
		}
		m.fail("add: %v", msg.err)
		ti := textinput.New()
		ti.Prompt = "+ "
		ti.Placeholder = "task title"
		ti.SetWidth(56)
		ti.SetValue(msg.title)
		m.add = &addState{input: ti, opts: msg.opts}
		m.mode = modeAdd
		return tea.Batch(m.add.input.Focus(), m.drainAfterAdd())
	}
	if m.prov.Live() {
		m.selectAfterReload = msg.id
		if next := m.drainAfterAdd(); next != nil {
			// Writes queued up behind this add; the drain's own reconcile
			// will deliver the new card and land the pending selection.
			m.note("added %s", msg.id)
			return next
		}
		if m.quitting {
			return tea.Quit
		}
		return m.reloadCmd("added " + msg.id)
	}
	// The mock's board already holds the task: no store round-trip to wait
	// for.
	m.reload()
	m.selectID(msg.id, true)
	m.note("added %s", msg.id)
	return m.drainAfterAdd()
}

// drainAfterAdd resumes whatever waited on the add: queued writes first,
// then further deferred adds. Nil when nothing waited.
func (m *Model) drainAfterAdd() tea.Cmd {
	if len(m.pending) > 0 {
		return m.firePersist()
	}
	if len(m.deferredAdds) > 0 {
		return m.fireAdd()
	}
	return nil
}

// addLayer draws the quick-add modal: the input plus the inherited-context
// chips, so nothing is stamped silently.
func (m *Model) addLayer() *lg.Layer {
	th := m.th
	a := m.add
	inner := clamp(m.w/3, 44, 72)

	var chips []string
	if a.opts.Lane != "" {
		chips = append(chips, "lane "+a.opts.Lane)
	} else {
		chips = append(chips, "lane (store default)")
	}
	if a.opts.Label != "" {
		chips = append(chips, "label "+a.opts.Label)
	}
	if a.opts.Epic != "" {
		chips = append(chips, "epic "+a.opts.Epic)
	}
	if a.opts.Repo != "" {
		chips = append(chips, "repo "+a.opts.Repo)
	} else {
		chips = append(chips, "repo (board auto)")
	}

	box := th.peek.Render(
		pad(th.peekHdr.Render("add item"), inner) + "\n\n" +
			a.input.View() + "\n\n" +
			th.accent.Render(pad("→ "+strings.Join(chips, " · "), inner)) + "\n" +
			th.dim.Render(pad("⏎ create · esc cancel · details via the edit menu", inner)))
	box = lg.NewStyle().MaxWidth(m.w).MaxHeight(m.h).Render(box)
	x := maxInt(0, (m.w-lg.Width(box))/2)
	y := maxInt(0, (m.h-lg.Height(box))/3)
	return lg.NewLayer(box).ID("add").X(x).Y(y).Z(zEdit)
}

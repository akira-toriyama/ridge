package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The SWEEP: furrow's maintenance passes (archive / tidy / unarchive) as one
// full-screen view — preview first, then ⏎ → a gate → ⏎ applies. sweep.go
// decided the rows; this file paints them, reads the previews and issues the
// writes.
//
// Every write here is STORE-FIRST (glossary): the frame keeps showing the
// pre-write preview until the write lands and both the board and the preview
// are re-read. The gate is the one place the user sees the count furrow will
// act on, because furrow itself moves an id list at exit 0 and prunes a whole
// class at exit 0 — the confirmation is the screen's, not furrow's.
//
// The archive write sends the EXPLICIT ids on screen (minus the ones `x`
// skipped), never the id-less sweep: what moves is what was previewed, even
// if more tasks aged between the read and the keystroke.

// sweepGate is a write waiting for its second ⏎.
type sweepGate struct {
	label string // the status-line name of the write ("archive 4 tasks")
	what  string // the gate's own line: what furrow will do
	run   func(board.Provider) error
}

// sweepResultMsg carries a preview read that ran off the UI thread.
type sweepResultMsg struct {
	seq int
	s   board.Sweep
	err error
}

func (m *Model) sweepCanvasH() int {
	return maxInt(1, m.h-fullTop-m.stripHeight()-footerH)
}

// openSweep enters the view and asks for the previews.
func (m *Model) openSweep() tea.Cmd {
	m.cancelDrag()
	m.view = viewSweep
	m.sweepScroll = 0
	m.sweepGate = nil
	m.note("sweep — furrow archive / tidy / unarchive · ⏎ previews the write, ⏎ again applies · x skips an archive row · esc returns")
	return m.loadSweep()
}

func (m *Model) closeSweep() {
	m.sweepGate = nil
	m.view = viewBoard
	m.note("board view")
}

// loadSweep fires the preview read: synchronously on the fixture (one
// deterministic -dump frame), as a Cmd on a live store. seq fences a stale
// result the way the filter's does.
func (m *Model) loadSweep() tea.Cmd {
	if m.queueBusy() {
		// The board's own rule for `r` (normalkeys.go): a read fired now would
		// race the queue's furrow process. sweepLoading stays UP so the header
		// says "reading…" rather than letting four empty sections claim there
		// is nothing to sweep, and every end of the drain — success, refusal,
		// with or without a reload — calls sweepAfterWrite, which reads then.
		m.sweepLoading = true
		m.note("sweep previews are read when the queued writes land")
		return nil
	}
	m.sweepSeq++
	seq := m.sweepSeq
	prov := m.prov
	if !prov.Live() {
		s, err := prov.SweepPreview()
		m.onSweepResult(sweepResultMsg{seq: seq, s: s, err: err})
		return nil
	}
	m.sweepLoading = true
	return func() tea.Msg {
		s, err := prov.SweepPreview()
		return sweepResultMsg{seq: seq, s: s, err: err}
	}
}

func (m *Model) onSweepResult(msg sweepResultMsg) {
	if msg.seq != m.sweepSeq {
		return
	}
	m.sweepLoading = false
	if msg.err != nil {
		// Keep the last good preview on screen and say the read failed: a
		// blank frame would read as "nothing to sweep", which is the one
		// claim a refused read cannot make.
		m.sweepErr = msg.err.Error()
		m.fail("sweep preview refused — %v", msg.err)
		return
	}
	m.sweepErr = ""
	s := msg.s
	m.sweep = &s
	// A candidate that left the list takes its skip mark with it, so a task
	// that ages back in later starts included like every other row.
	for id := range m.sweepSkip {
		found := false
		for _, t := range s.Archivable {
			if t.ID == id {
				found = true
				break
			}
		}
		if !found {
			delete(m.sweepSkip, id)
		}
	}
	rows := sweepRows(m.sweep)
	if sweepIndex(rows, m.sweepSel) < 0 {
		m.sweepSel = sweepFirst(rows)
	}
}

// ---- rendering --------------------------------------------------------------

func (m *Model) renderSweep() string {
	rows := sweepRows(m.sweep)
	if sweepIndex(rows, m.sweepSel) < 0 {
		m.sweepSel = sweepFirst(rows)
	}
	lines := m.sweepLines(rows, maxInt(1, m.w-2))
	canvasH := m.sweepCanvasH()
	sel := sweepIndex(rows, m.sweepSel)
	m.sweepScroll = scrollToSel(m.sweepScroll, len(lines), canvasH, func() (int, int, bool) {
		if sel < 0 {
			return 0, 0, false
		}
		top := sel
		if sel > 0 && rows[sel-1].Header {
			top = sel - 1
		}
		return top, sel, true
	})
	shown := lines
	if len(shown) > canvasH {
		shown = shown[m.sweepScroll:minInt(len(lines), m.sweepScroll+canvasH)]
	}
	return m.composeFullScreen(m.sweepTitleBar(), m.sweepHeader(len(lines) > canvasH),
		m.fillCanvas(shown, canvasH), func(h int) string { return m.sweepStrip(rows, h) })
}

func (m *Model) sweepTitleBar() string {
	n := 0
	if s := m.sweep; s != nil {
		n = len(s.Archivable) + len(s.DoneDeps) + len(s.UnknownKeys) + len(s.Archived)
	}
	return m.fullScreenTitleBar(viewSweep, fmt.Sprintf("%d rows", n), "⟨SWEEP⟩")
}

// sweepHeader states the counts each pass would act on — furrow's numbers,
// summed from its own previews — and, while a gate is open, what the second
// ⏎ will do. The gate line REPLACES the counts rather than joining them: it is
// the one line the user must read before the keystroke.
func (m *Model) sweepHeader(clipped bool) string {
	th := m.th
	if g := m.sweepGate; g != nil {
		left := th.warn.Render("⏎ confirms: "+g.what) + th.dim.Render("  ·  any other key cancels (ctrl+c quits)")
		return joinEnds(left, "", m.w)
	}
	left := th.peekHdr.Render("sweep") + th.dim.Render("  ·  furrow archive / tidy / unarchive")
	var bits []string
	switch {
	case m.sweepLoading && m.sweep == nil:
		bits = append(bits, th.dim.Render("reading the previews…"))
	case m.sweep != nil:
		s := m.sweep
		included := len(sweepArchiveSet(s, m.sweepSkip))
		arch := fmt.Sprintf("%d archivable (closed >%dd)", len(s.Archivable), s.OlderThanDays)
		if included != len(s.Archivable) {
			arch += fmt.Sprintf(", %d skipped", len(s.Archivable)-included)
		}
		bits = append(bits, arch,
			fmt.Sprintf("%d with satisfied deps", len(s.DoneDeps)),
			fmt.Sprintf("%d with unknown keys", len(s.UnknownKeys)),
			th.dim.Render(fmt.Sprintf("%d archived", len(s.Archived))))
	}
	if m.sweepErr != "" {
		msg := "last preview refused — showing the previous one"
		if m.sweep == nil {
			msg = m.sweepErr
		}
		bits = append(bits, th.warn.Render(msg))
	}
	if m.sweepLoading && m.sweep != nil {
		bits = append(bits, th.dim.Render("re-reading…"))
	}
	if clipped {
		bits = append(bits, th.dim.Render("^u/^d page"))
	}
	return joinEnds(left, th.dim.Render(strings.Join(bits, " · ")), m.w)
}

// sweepLines renders every row to one screen line of width w.
func (m *Model) sweepLines(rows []sweepRow, w int) []string {
	th := m.th
	out := make([]string, 0, len(rows))
	byID := map[string]board.SweepTask{}
	deps := map[string][]string{}
	unknown := map[string]board.TidyUnknownKey{}
	if s := m.sweep; s != nil {
		for _, t := range s.Archivable {
			byID[sweepKey(sweepArchive, t.ID)] = t
		}
		for _, t := range s.Archived {
			byID[sweepKey(sweepArchived, t.ID)] = t
		}
		for _, d := range s.DoneDeps {
			deps[d.ID] = d.Deps
		}
		for i, u := range s.UnknownKeys {
			id := u.ID
			if id == "" {
				id = fmt.Sprintf("%s#%d", u.File, i)
			}
			unknown[id] = u
		}
	}
	for _, r := range rows {
		switch {
		case r.Header:
			out = append(out, m.sweepHeaderLine(r.Section, w))
		case r.Empty:
			text := sweepEmptyText(r.Section)
			if m.sweep == nil {
				// No verdict yet (first read pending, or deferred behind the
				// queue): an "empty" section must not claim emptiness.
				text = "— not read yet —"
			}
			out = append(out, "   "+th.dim.Render(text))
		default:
			gutter := "  "
			if r.Key == m.sweepSel {
				gutter = th.accent.Render("▌") + " "
			}
			var body string
			switch r.Section {
			case sweepArchive:
				body = m.sweepTaskLine(byID[r.Key], m.sweepSkip[r.ID], w-2)
			case sweepArchived:
				body = m.sweepTaskLine(byID[r.Key], false, w-2)
			case sweepDoneDeps:
				title := ""
				if t := m.b.Task(r.ID); t != nil {
					title = t.Title
				}
				tag := th.dim.Render(" ← done: ") + th.chipAlt.Render(strings.Join(deps[r.ID], ","))
				body = th.muted.Render(r.ID) + "  " + ansi.Truncate(title, maxInt(4, w-2-lg.Width(r.ID)-2-lg.Width(tag)), "…") + tag
			case sweepUnknownKeys:
				u := unknown[r.ID]
				body = th.muted.Render(u.File) + "  " + th.chipAlt.Render(strings.Join(u.Keys, ","))
			}
			// The gutter bar alone marks the selection (the box overview's
			// rule): the row is a composition of styled fragments, and
			// wrapping it in one more style fights every reset inside.
			out = append(out, gutter+pad(body, maxInt(1, w-2)))
		}
	}
	return out
}

func (m *Model) sweepHeaderLine(sec sweepSection, w int) string {
	th := m.th
	var label, verb string
	switch sec {
	case sweepArchive:
		label, verb = "archive — done tasks past the age guard", "⏎ archives the included rows · x skips/includes one"
	case sweepDoneDeps:
		label, verb = "tidy done-deps — open tasks with satisfied dep edges", "⏎ prunes every listed edge"
	case sweepUnknownKeys:
		label, verb = "tidy unknown-keys — shards carrying keys this furrow does not know", "⏎ drops every listed key"
	case sweepArchived:
		label, verb = "archived — the archive store", "⏎ restores the row to the done lane"
	}
	head := th.rule.Render("──") + th.peekHdr.Render(" "+label+" ") + th.dim.Render(verb+" ")
	if n := w - lg.Width(head); n > 0 {
		head += th.rule.Render(strings.Repeat("─", n))
	}
	return ansi.Truncate(head, w, "")
}

func sweepEmptyText(sec sweepSection) string {
	switch sec {
	case sweepArchive:
		return "— nothing old enough to archive —"
	case sweepDoneDeps:
		return "— no satisfied dep edges —"
	case sweepUnknownKeys:
		return "— no unknown keys parked —"
	case sweepArchived:
		return "— the archive is empty —"
	}
	return ""
}

// sweepTaskLine is one archive / archived row: the marker, id, the closed
// age, the repos, and the title with whatever width is left — the suffix is
// composed first so a CJK title's ellipsis never eats the numbers.
func (m *Model) sweepTaskLine(t board.SweepTask, skipped bool, w int) string {
	th := m.th
	mark := th.ok.Render("✓")
	if skipped {
		mark = th.dim.Render("·")
	}
	suffix := ""
	if !t.Closed.IsZero() {
		suffix += th.dim.Render("closed " + ago(t.Closed))
	}
	if len(t.Repos) > 0 {
		suffix += th.dim.Render("  ") + th.chip.Render(strings.Join(t.Repos, ","))
	}
	head := mark + " " + th.muted.Render(t.ID) + "  "
	room := w - lg.Width(head) - lg.Width(suffix) - 2
	title := ansi.Truncate(t.Title, maxInt(4, room), "…")
	if skipped {
		title = th.dim.Render(title)
	}
	return head + title + strings.Repeat(" ", maxInt(1, room-lg.Width(title)+2)) + suffix
}

// sweepStrip is the strip below the canvas: the row under the cursor in one
// line more than its row can carry (the full title, the repos).
func (m *Model) sweepStrip(rows []sweepRow, h int) string {
	i := sweepIndex(rows, m.sweepSel)
	if i < 0 || m.sweep == nil {
		return strings.Repeat("\n", maxInt(0, h-1))
	}
	r := rows[i]
	var line string
	switch r.Section {
	case sweepArchive, sweepArchived:
		for _, t := range append(append([]board.SweepTask(nil), m.sweep.Archivable...), m.sweep.Archived...) {
			if sweepKey(r.Section, t.ID) == r.Key {
				line = t.ID + "  " + t.Title
				if len(t.Repos) > 0 {
					line += "  " + strings.Join(t.Repos, ",")
				}
			}
		}
	case sweepDoneDeps:
		if t := m.b.Task(r.ID); t != nil {
			line = t.ID + "  " + t.Title
		}
	case sweepUnknownKeys:
		line = r.ID
	}
	out := []string{" " + ansi.Truncate(m.th.dim.Render(line), maxInt(1, m.w-2), "…")}
	for len(out) < h {
		out = append(out, "")
	}
	return strings.Join(out[:h], "\n")
}

// ---- keys -------------------------------------------------------------------

func (m *Model) onSweepKey(msg tea.KeyPressMsg) tea.Cmd {
	rows := sweepRows(m.sweep)

	if g := m.sweepGate; g != nil {
		// The gate: ⏎ applies, ANY other key cancels — including the arrows,
		// because a cursor that moved under an open gate would leave the gate
		// naming a row the eye is no longer on. ctrl+c stays the escape hatch
		// it is everywhere else (the epic overlay checks it before its stage
		// machine for the same reason): it quits, it does not merely cancel.
		if key.Matches(msg, m.keys.ForceQuit) {
			m.sweepGate = nil
			return m.quitOrFlush()
		}
		m.sweepGate = nil
		if key.Matches(msg, m.keys.Commit) {
			return m.sweepWrite(g)
		}
		m.note("%s — cancelled", g.label)
		return nil
	}

	switch {
	case key.Matches(msg, m.keys.Cancel):
		if m.fullHelp {
			m.fullHelp = false
			return nil
		}
		m.closeSweep()

	case key.Matches(msg, m.keys.Sweep), key.Matches(msg, m.keys.View):
		m.closeSweep()

	case key.Matches(msg, m.keys.Quit):
		return m.quitOrFlush()

	case key.Matches(msg, m.keys.Reload):
		m.note("re-reading the sweep previews")
		return m.loadSweep()

	case key.Matches(msg, m.keys.Commit):
		return m.armSweepGate(rows)

	case key.Matches(msg, m.keys.SweepSkip):
		i := sweepIndex(rows, m.sweepSel)
		if i < 0 || rows[i].Section != sweepArchive {
			m.note("x skips an ARCHIVE row; tidy prunes a whole class and restore is one row at a time")
			return nil
		}
		id := rows[i].ID
		if m.sweepSkip == nil {
			m.sweepSkip = map[string]bool{}
		}
		if m.sweepSkip[id] {
			delete(m.sweepSkip, id)
			m.note("%s included in the archive write", id)
		} else {
			m.sweepSkip[id] = true
			m.note("%s skipped — it stays on the board", id)
		}

	case key.Matches(msg, m.keys.Up):
		m.sweepSel = sweepStep(rows, m.sweepSel, -1)
	case key.Matches(msg, m.keys.Down):
		m.sweepSel = sweepStep(rows, m.sweepSel, +1)
	case key.Matches(msg, m.keys.Top):
		m.sweepSel = sweepFirst(rows)
	case key.Matches(msg, m.keys.Bottom):
		m.sweepSel = sweepLast(rows)
	case key.Matches(msg, m.keys.PeekScroll):
		m.halfPage(msg, m.sweepCanvasH(), func(dir int) bool {
			at := m.sweepSel
			m.sweepSel = sweepStep(rows, m.sweepSel, dir)
			return m.sweepSel != at
		}, "the sweep")

	case key.Matches(msg, m.keys.Help):
		m.fullHelp = !m.fullHelp
	}
	return nil
}

// armSweepGate is the first ⏎: it names the write the row's section means and
// waits for the second. Refused outright when nothing would happen, so a gate
// never opens over a no-op.
func (m *Model) armSweepGate(rows []sweepRow) tea.Cmd {
	i := sweepIndex(rows, m.sweepSel)
	if i < 0 || m.sweep == nil {
		m.note("nothing under the cursor to sweep")
		return nil
	}
	r := rows[i]
	var g sweepGate
	switch r.Section {
	case sweepArchive:
		ids := sweepArchiveSet(m.sweep, m.sweepSkip)
		if len(ids) == 0 {
			m.note("every archive row is skipped — x includes one first")
			return nil
		}
		g = sweepGate{
			label: fmt.Sprintf("archive %d task(s)", len(ids)),
			what:  fmt.Sprintf("furrow archive %s --yes — %d done task(s) leave the board for .furrow/archive/ (unarchive brings one back)", sweepIDsBrief(ids), len(ids)),
			run:   func(p board.Provider) error { return p.Archive(ids) },
		}
	case sweepDoneDeps:
		n := 0
		for _, d := range m.sweep.DoneDeps {
			n += len(d.Deps)
		}
		g = sweepGate{
			label: fmt.Sprintf("tidy done-deps (%d edge(s))", n),
			what:  fmt.Sprintf("furrow tidy --done-deps --yes — ALL %d satisfied dep edge(s) on %d task(s) are pruned (no per-edge form; updated does not move)", n, len(m.sweep.DoneDeps)),
			run:   func(p board.Provider) error { return p.Tidy(board.TidyDoneDeps) },
		}
	case sweepUnknownKeys:
		n := 0
		for _, u := range m.sweep.UnknownKeys {
			n += len(u.Keys)
		}
		g = sweepGate{
			label: fmt.Sprintf("tidy unknown-keys (%d key(s))", n),
			what:  fmt.Sprintf("furrow tidy --unknown-keys --yes — ALL %d parked key(s) on %d record(s) are dropped, a key a NEWER furrow wrote included", n, len(m.sweep.UnknownKeys)),
			run:   func(p board.Provider) error { return p.Tidy(board.TidyUnknownKeys) },
		}
	case sweepArchived:
		id := r.ID
		g = sweepGate{
			label: "unarchive " + id,
			what:  fmt.Sprintf("furrow unarchive %s — back to the done lane exactly as archived (to reopen, move it afterwards)", id),
			run:   func(p board.Provider) error { return p.Unarchive([]string{id}) },
		}
	}
	m.sweepGate = &g
	m.note("%s — ⏎ confirms, any other key cancels", g.label)
	return nil
}

// sweepIDsBrief spells an id list for the gate line without letting a long
// list push the count off the frame.
func sweepIDsBrief(ids []string) string {
	if len(ids) <= 4 {
		return strings.Join(ids, " ")
	}
	return strings.Join(ids[:3], " ") + fmt.Sprintf(" … +%d", len(ids)-3)
}

// sweepWrite queues the gated write store-first and re-reads the previews
// when it lands (persist.go's sweep hook). The refusals are the store-first
// family's: a write in flight, or one landed and not yet re-read.
func (m *Model) sweepWrite(g *sweepGate) tea.Cmd {
	prov := m.prov
	return m.storeFirstWrite(persistOp{
		label: "sweep: " + g.label,
		run:   func() ([]string, error) { return nil, g.run(prov) },
		// A sweep write can fail AFTER furrow moved something (the adapter
		// refuses a reply that names fewer tasks than it sent), so a refusal
		// must still re-read: the board would otherwise keep showing tasks
		// that are already in the archive until a manual `r`.
		reloadOnFail: true,
	}, "a sweep write")
}

// sweepAfterWrite is what every end of the persist queue owes this view: a
// fresh preview. Nil when the sweep is not on screen — the previews are read
// on open. Called from onReloadDone (a re-read applied), from the fixture's
// drain end (which reloads nothing) and from the refusal branch that
// re-reads nothing. The one end that reads nothing is a re-read that itself
// FAILED (onReloadDone's error paths): sweepReadStalled drops the "reading…"
// claim there and names `r`, which re-reads the previews from inside the view.
func (m *Model) sweepAfterWrite() tea.Cmd {
	if m.view != viewSweep || m.queueBusy() {
		return nil
	}
	return m.loadSweep()
}

// sweepReadStalled is the board re-read failing under a deferred sweep read:
// nothing more is coming, so the header must stop saying "reading…" and the
// frame must say how to get the previews (measured: without this the header
// claimed a read in flight forever).
func (m *Model) sweepReadStalled() {
	if m.view != viewSweep || !m.sweepLoading {
		return
	}
	m.sweepLoading = false
	if m.sweep == nil {
		m.sweepErr = "previews not read — r reads them"
	}
}

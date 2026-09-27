package ui

import (
	"fmt"
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
)

// The DELETE gate: the `delete` row of the task edit overlay and of the epic
// overlay — `furrow rm` / `furrow epic rm` behind one preview-first gate.
// Opening the row READS furrow's preview (the dry run: the target as it is,
// what still points at it, what happens to its assets) and shows it; ⏎ then
// deletes — unless something references the target, in which case the first
// ⏎ only ARMS --force and a second one severs and deletes. furrow would refuse
// the plain delete anyway (kind `referenced`, exit 2), so the arm step is the
// screen's, not furrow's: it puts the list of what --force will sever in
// front of the keystroke that severs it, the same before-the-press doctrine
// as the epic overlay's close gate.
//
// The write is store-first (glossary): the overlay closes on the keystroke,
// the card or row stays until the write lands and the board re-reads, and the
// landing note carries what the apply reported — references severed, a series
// ended, assets kept. A refusal after the preview — a reference that
// appeared in between, or a target another writer dropped (a miss, exit 1)
// — is furrow's own message on the status line, which names a body by its
// file path where the gate named it by id; the preview is re-read by
// reopening the row, and a dropped target closes the overlay at the next
// re-read (editTask).
//
// The gate's states and their headless frames (-demo): nothing points at it
// (rm), referenced and disarmed (rmreferenced), armed (rmforce), a series
// the removal ends (rmrepeat), a box (epicrm), and the two a live store alone
// reaches — the read in flight (rmwait) and a refused read (rmrefused) —
// which the demos can on the fixture's synchronous read.

// rmTarget names what a gate is about: one task, or one box.
type rmTarget struct {
	id   string
	epic bool
}

// label is the persist queue's name for the write, furrow's verb and the id.
func (t rmTarget) label() string {
	if t.epic {
		return "epic rm " + t.id
	}
	return "rm " + t.id
}

func (t rmTarget) noun() string {
	if t.epic {
		return "box"
	}
	return "task"
}

// rmState is one gate's state, held by the overlay whose row opened it and
// reset every time the row is opened.
type rmState struct {
	target  rmTarget
	report  *board.RemoveReport // the preview; nil until read
	err     string              // a refused read; "" otherwise
	loading bool
	seq     int  // Model.rmSeq at the read: fences a stale one, as the sweep's does
	armed   bool // --force: the second ⏎ on a referenced target
}

// rmPreviewMsg carries a preview read that ran off the UI thread.
type rmPreviewMsg struct {
	target rmTarget
	seq    int
	report board.RemoveReport
	err    error
}

// openRmGate resets the gate and fires the preview: synchronously on the
// fixture (one deterministic -dump frame), as a Cmd on a live store. While a
// write is in flight no read is fired — the board's own rule for `r`
// (normalkeys.go): the read would race the queue's furrow process — and the
// gate says so instead of showing a report it does not have.
//
// The preview is the --force dry run. Without --force furrow refuses the
// dry run of a referenced target outright (measured on furrow dev
// 2026-09-27: exit 2, kind referenced, the summary in the message and the
// list in details), so a plain preview would show a report for the easy
// case and an error for the one the gate exists for. A dry run severs
// nothing: --force there only means "list what --force would sever", which
// is exactly the list the arm step puts in front of the second ⏎.
func (m *Model) openRmGate(st *rmState, target rmTarget) tea.Cmd {
	m.rmSeq++
	seq := m.rmSeq
	*st = rmState{target: target, seq: seq, loading: true}
	if m.queueBusy() {
		st.loading = false
		st.err = "a write is still in flight — esc out, let it land, then reopen the row"
		m.noteRmGate(st)
		return nil
	}
	prov := m.prov
	read := func() (board.RemoveReport, error) {
		if target.epic {
			return prov.EpicRemove(target.id, board.RemoveOptions{Force: true})
		}
		return prov.Remove([]string{target.id}, board.RemoveOptions{Force: true})
	}
	if !prov.Live() {
		rep, err := read()
		m.onRmPreview(rmPreviewMsg{target: target, seq: seq, report: rep, err: err})
		return nil
	}
	m.noteRmGate(st)
	return func() tea.Msg {
		rep, err := read()
		return rmPreviewMsg{target: target, seq: seq, report: rep, err: err}
	}
}

// rmGateOf is the gate a preview belongs to: the overlay still open on that
// target with its delete row in the gate stage — nil once the overlay has
// moved on, which is how a late read is dropped.
func (m *Model) rmGateOf(target rmTarget) *rmState {
	if target.epic {
		if e := m.epic; e != nil && e.id == target.id && e.stage == stageGate && e.field == epicFieldDelete {
			return &e.rm
		}
		return nil
	}
	if e := m.edit; e != nil && e.id == target.id && e.stage == stageGate && e.field == fieldDelete {
		return &e.rm
	}
	return nil
}

func (m *Model) onRmPreview(msg rmPreviewMsg) {
	st := m.rmGateOf(msg.target)
	if st == nil || msg.seq != st.seq {
		return
	}
	st.loading = false
	if msg.err != nil {
		st.err = msg.err.Error()
	} else {
		rep := msg.report
		st.report = &rep
	}
	m.noteRmGate(st)
}

// noteRmGate keeps the status row true for the gate's state. Same contract
// as the overlays' own notes: a refusal nobody has read yet is not
// overwritten.
func (m *Model) noteRmGate(st *rmState) {
	if m.statusErr {
		return
	}
	l := st.target.label()
	switch {
	case st.loading:
		m.note("%s — reading what points at it…", l)
	case st.err != "":
		m.fail("%s — no preview: %s · esc backs out", l, st.err)
	case st.report == nil:
		m.note("%s — esc backs out", l)
	case st.report.References.Empty():
		m.note("%s — nothing points at it · ⏎ deletes · esc backs out", l)
	case !st.armed:
		m.note("%s — %d reference(s) stand · ⏎ arms --force · esc backs out", l, st.report.References.Count())
	default:
		m.note("%s — --force armed · ⏎ severs %d reference(s) and deletes · esc backs out", l, st.report.References.Count())
	}
}

// onRmGateKey is any key in a delete row's gate stage. Only ⏎ acts; the
// shell already answered esc. On a referenced target the first ⏎ arms
// --force and re-notes; the ⏎ that writes closes the overlay, because its
// target is about to stop resolving and an overlay over a vanishing row would
// only close itself at the re-read anyway (editTask).
func (m *Model) onRmGateKey(msg tea.KeyPressMsg, st *rmState) tea.Cmd {
	if !key.Matches(msg, m.keys.Commit) {
		return nil
	}
	switch {
	case st.loading:
		m.note("%s — still reading what points at it", st.target.label())
		return nil
	case st.err != "":
		m.fail("%s — no preview to act on: %s", st.target.label(), st.err)
		return nil
	case st.report == nil:
		return nil
	}
	if !st.report.References.Empty() && !st.armed {
		st.armed = true
		m.noteRmGate(st)
		return nil
	}
	target, force := st.target, st.armed
	prov := m.prov
	note := new(string)
	// reloadOnFail: a refusal says the board under the gate is not the
	// store's (the target withdrawn elsewhere: not-found), so the re-read is
	// owed now, as for every store-first box write (t-2wa3).
	op := persistOp{label: target.label(), noLocal: true, note: note, reloadOnFail: true, run: func() ([]string, error) {
		var rep board.RemoveReport
		var err error
		if target.epic {
			rep, err = prov.EpicRemove(target.id, board.RemoveOptions{Force: force, Apply: true})
		} else {
			rep, err = prov.Remove([]string{target.id}, board.RemoveOptions{Force: force, Apply: true})
		}
		if err == nil {
			*note = rmLandingNote(rep)
		}
		return nil, err
	}}
	queued := len(m.pending)
	cmd := m.storeFirstWrite(op, "a delete")
	if len(m.pending) == queued {
		// Refused (in flight, unread, or rolling back), and the refusal is on
		// the status line — the gate stays so the user can retry from it.
		return cmd
	}
	if target.epic {
		m.exitEpic()
	} else {
		m.exitEdit()
	}
	// exitEpic hands the keyboard back to the slice panel and re-notes ITS
	// keys; the wait is the fact that matters until the write lands.
	m.note("%s — waiting for furrow", target.label())
	return cmd
}

// rmLandingNote is the prose the write computed, for the landing note: what
// --force severed, the series a removal ended, the assets that went and the
// ones another body kept. "" when the report has nothing beyond the deletion
// itself, and the label alone lands.
func rmLandingNote(rep board.RemoveReport) string {
	var parts []string
	if n := rep.References.Count(); n > 0 {
		parts = append(parts, fmt.Sprintf("severed %d reference(s)", n))
	}
	for _, t := range rep.Tasks {
		if t.Repeat != "" {
			parts = append(parts, "series ended ("+t.ID+")")
		}
	}
	if n := len(rep.Assets.Deleted); n > 0 {
		parts = append(parts, fmt.Sprintf("%d asset(s) deleted", n))
	}
	if n := len(rep.Assets.Kept); n > 0 {
		parts = append(parts, fmt.Sprintf("%d asset(s) kept (held by %s)", n, strings.Join(assetHolders(rep.Assets.Kept), ", ")))
	}
	return strings.Join(parts, " · ")
}

func assetHolders(kept []board.KeptAsset) []string {
	seen := map[string]bool{}
	var out []string
	for _, k := range kept {
		for _, id := range k.HeldBy {
			if !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// rmReferenceLines is the reference list under the summary, one edge per
// line in furrow's own `rm` output order (cmd_rm.go printReferences: dep,
// member, epic dep, link) and near its words — `→` for its `->`, and a body
// named by its owner's id where it prints the file path. Capped: a box of
// eighteen members is a summary plus a count, not a scroll, and the
// summary above still counts every one (the footer counts them too).
const rmReferenceCap = 6

func rmReferenceLines(r board.References) []string {
	var lines []string
	for _, d := range r.Deps {
		lines = append(lines, "dep      "+d.From+" → "+d.To)
	}
	for _, mb := range r.Members {
		lines = append(lines, "member   "+mb.Task+" in "+mb.Epic+" (unfiled)")
	}
	for _, d := range r.EpicDeps {
		lines = append(lines, "epic dep "+d.From+" → "+d.To)
	}
	for _, l := range r.Links {
		lines = append(lines, "link     "+l.Body+" [["+l.To+"]]")
	}
	if len(lines) > rmReferenceCap {
		more := len(lines) - rmReferenceCap
		lines = append(lines[:rmReferenceCap], fmt.Sprintf("… +%d more", more))
	}
	return lines
}

// renderRmGate draws the gate: the target, what a withdrawal is, the preview
// (or why there is none), and the one line saying what ⏎ does right now.
func (m *Model) renderRmGate(st *rmState, title string, inner int) string {
	th := m.th
	var b strings.Builder
	line := func(s string, style func(...string) string) {
		for _, l := range wrapLines(s, inner) {
			b.WriteString(style(pad(l, inner)) + "\n")
		}
	}
	b.WriteString(th.peekHdr.Render("delete this "+st.target.noun()) + "\n\n")
	line(st.target.id+" "+title, th.base.Render)
	b.WriteString("\n")
	verb := "furrow rm"
	if st.target.epic {
		verb = "furrow epic rm"
	}
	line(verb+" withdraws the record — shard, body and assets — and nothing brings it back but the board repo's git history. It is not archive's round trip; everyday parking is the icebox lane.", th.muted.Render)
	b.WriteString("\n")
	switch {
	case st.loading:
		line("reading what points at it…", th.dim.Render)
	case st.err != "":
		line("no preview: "+st.err, th.danger.Render)
	case st.report != nil:
		rep := st.report
		for _, t := range rep.Tasks {
			if t.Repeat == "" {
				continue
			}
			when := ""
			if !t.Due.IsZero() {
				when = ", due " + t.Due.In(board.Zone()).Format("2006-01-02")
			}
			line("repeat: carries a series ("+t.Repeat+when+") — removing it ends the series; no successor is minted", th.warn.Render)
		}
		if rep.References.Empty() {
			line("nothing points at it", th.muted.Render)
		} else {
			// wrapJoin over the summary's `; ` parts, not wrapLines: an id
			// list breaks after its hyphens under wrapLines (t-7wdg, t-
			// 9m2q), and an id split across lines cannot be read or copied.
			parts := strings.Split("still referenced — "+rep.References.Summary(), "; ")
			for _, l := range strings.Split(wrapJoin(parts, "; ", inner), "\n") {
				b.WriteString(th.warn.Render(pad(l, inner)) + "\n")
			}
			for _, l := range rmReferenceLines(rep.References) {
				line(l, th.muted.Render)
			}
		}
		if rep.Epic != nil && rep.Epic.Active {
			// furrow withdraws the active box at exit 0 with no word about
			// the slot (measured on dev 2026-09-27), so this line is the
			// whole warning — the overlay's rule that a precondition is
			// stated before the press, not after. Off the report, not the
			// board: the board may be minutes staler than the read.
			line("This is the ACTIVE box: withdrawing it vacates its repo slot, and furrow says nothing about that.", th.warn.Render)
		}
		if n, k := len(rep.Assets.Deleted), len(rep.Assets.Kept); n > 0 || k > 0 {
			s := fmt.Sprintf("assets: %d deleted", n)
			if k > 0 {
				s += fmt.Sprintf(" · %d kept (held by %s)", k, strings.Join(assetHolders(rep.Assets.Kept), ", "))
			}
			line(s, th.muted.Render)
		}
	}
	b.WriteString("\n" + th.dim.Render(pad(rmFooter(st), inner)))
	return b.String()
}

// rmFooter is the gate's own key line — what ⏎ does in THIS state, never a
// promise the next frame contradicts.
func rmFooter(st *rmState) string {
	switch {
	case st.loading:
		return "⏎ waits for the read · esc backs out"
	case st.err != "" || st.report == nil:
		return "esc backs out"
	case st.report.References.Empty():
		return "⏎ deletes · esc backs out"
	case !st.armed:
		if n := st.report.References.Count(); n > 1 {
			return fmt.Sprintf("⏎ arms --force (severs all %d) · esc backs out", n)
		}
		return "⏎ arms --force (severs the one reference) · esc backs out"
	}
	return fmt.Sprintf("⏎ severs %d reference(s) and deletes · esc backs out", st.report.References.Count())
}

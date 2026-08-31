package ui

import (
	"fmt"
	"github.com/akira-toriyama/ridge/internal/board"
	"strings"
	"time"

	lg "charm.land/lipgloss/v2"
	"charm.land/lipgloss/v2/tree"
	"github.com/charmbracelet/x/ansi"
)

// The detail side-peek. It is drawn as a lipgloss Layer at zPeek over the
// board rather than as a split pane, which is the point: the compositor lets a
// panel float above content it does not have to reflow.

// peekBox is the panel's place on screen. Every dimension is clamped to the
// terminal: the old floor of 6 rows anchored at y=2 hung off the bottom of any
// frame shorter than 8 rows, and unlike the help overlay the peek had no
// backstop of its own.
func (m *Model) peekBox() (x, y, w, h int) {
	tw, th := maxInt(m.w, 1), maxInt(m.h, 1)
	w = minInt(clamp(tw/2, 46, 70), tw)
	x = tw - w
	y = minInt(rowColHdr, th-1)
	h = clamp(th-footerH-y, 1, th-y)
	return x, y, w, h
}

// syncPeek refreshes the peek's viewport for the current selection.
func (m *Model) syncPeek() {
	if !m.peekOpen {
		return
	}
	_, _, w, h := m.peekBox()
	m.vp.SetWidth(maxInt(10, w-4))
	m.vp.SetHeight(maxInt(3, h-2))
	m.vp.SetContent(m.peekContent(maxInt(10, w-4)))
}

func (m *Model) peekLayer() *lg.Layer {
	x, y, w, h := m.peekBox()
	// Width/Height are TOTALS in lipgloss v2 — border and padding included. Pass
	// the outer box, not the inner: sizing this to w-2 makes the panel re-wrap
	// the viewport's already-fitted output, which is what put half of every
	// dependency line on its own row.
	body := m.th.peek.Width(w).Height(h).Render(m.vp.View())
	return lg.NewLayer(body).ID("peek").X(x).Y(y).Z(zPeek)
}

// peekContent is the whole detail document. Sections are ordered by how often
// you need them: identity, metadata, DEPENDENCIES, checklist, prose.
func (m *Model) peekContent(w int) string {
	th := m.th
	t := m.curTask()
	if t == nil {
		return th.dim.Render("no task selected")
	}
	var b strings.Builder

	b.WriteString(joinEnds(th.peekHdr.Render(t.ID), th.chipAlt.Render(t.Status), w) + "\n")
	for _, l := range wrapLines(t.Title, w) {
		b.WriteString(th.base.Render(l) + "\n")
	}
	b.WriteString("\n")

	var meta []string
	if t.Value > 0 || t.Effort > 0 {
		meta = append(meta, fmt.Sprintf("value %d", t.Value), fmt.Sprintf("effort %d", t.Effort))
	}
	switch {
	case m.g.Actionable(t.ID):
		meta = append(meta, th.ok.Render(glyphActionable+" actionable"))
	case len(m.g.BlockedBy(t.ID)) > 0:
		meta = append(meta, th.danger.Render(fmt.Sprintf("%s blocked", glyphBlocked)))
	}
	if len(meta) > 0 {
		b.WriteString(th.muted.Render(wrapJoin(meta, " · ", w)) + "\n")
	}

	var meta2 []string
	if len(t.Repos) > 0 {
		meta2 = append(meta2, "repos "+strings.Join(t.Repos, ","))
	} else {
		meta2 = append(meta2, th.dim.Render("draft (no repo)"))
	}
	if t.Epic != "" {
		label := t.Epic
		if e := m.b.Epic(t.Epic); e != nil {
			// Progress before the title, like the dep lines below: the
			// numbers must survive a CJK title's ellipsis.
			label = fmt.Sprintf("%s (%d/%d) %s", t.Epic, e.Done, e.Total, e.Title)
			if e.Stuck {
				label += " " + th.warn.Render("STUCK")
			}
			// furrow lints an open task under a closed box (epic-closed), and
			// the --all read is what lets ridge say so instead of rendering it
			// as an ordinary membership.
			if !e.Closed.IsZero() {
				label += " " + th.dim.Render("(closed)")
			}
		}
		meta2 = append(meta2, "epic "+label)
	}
	if len(t.Labels) > 0 {
		meta2 = append(meta2, "labels "+strings.Join(t.Labels, ","))
	}
	b.WriteString(th.muted.Render(wrapJoin(meta2, " · ", w)) + "\n")
	// The epic's own dep edges, resolved — `furrow epic dep --list`'s "waits
	// on", in its wording. Gated on furrow's derived open_deps, the same
	// field the slice panel counts for →N, so the two surfaces cannot
	// disagree about whether a box still waits; with every dep satisfied
	// there is no line, exactly as there is no arrow.
	//
	// Three states, and furrow names all three itself: `epic dep --list`
	// prints [open], [closed] and [?]. Over the --all read they are decidable
	// from what the board holds — measured on v5.0.0 with one box carrying all
	// three at once, open_deps was exactly the OPEN dep, excluding both the
	// closed and the dangling one. So this renders (closed) for a resolved-away
	// dep the board holds as closed, and (missing) — furrow's lint code is
	// epic-dep-missing, at severity ERROR — for one it cannot resolve at all.
	// Calling that second one "satisfied" would put a reassuring word on a
	// broken reference.
	//
	// "(satisfied)" survives for the case the measurement says cannot happen:
	// furrow settled a dep whose box this board still shows OPEN. It has no
	// fixture site because no board furrow produces has one, and it says the
	// weakest true thing rather than inventing a reason.
	if e := m.b.Epic(t.Epic); e != nil && len(e.OpenDeps) > 0 {
		open := make(map[string]bool, len(e.OpenDeps))
		for _, d := range e.OpenDeps {
			open[d] = true
		}
		parts := []string{"epic waits on"}
		for _, d := range e.Deps {
			de := m.b.Epic(d)
			switch {
			case !open[d] && de == nil:
				parts = append(parts, d+" (missing)")
			case !open[d] && !de.Closed.IsZero():
				// Resolved, so the numbers and the title are available — and
				// they are the half a CJK ellipsis must not eat, exactly as on
				// the waiting rows below.
				parts = append(parts, fmt.Sprintf("%s (%d/%d) %s (closed)", d, de.Done, de.Total, de.Title))
			case !open[d]:
				parts = append(parts, d+" (satisfied)")
			case de == nil:
				// Open per furrow's verdict but not in this read's epic
				// set: show the raw id rather than inventing a state.
				parts = append(parts, d)
			case de.Stuck:
				// warn, like the own-epic line's STUCK: a marker that
				// reads as dim body text is the one thing it must not be.
				parts = append(parts, fmt.Sprintf("%s (%d/%d) %s %s",
					d, de.Done, de.Total, th.warn.Render("STUCK"), de.Title))
			default:
				// Progress BEFORE the title: a CJK epic title routinely
				// overflows the box and truncates, and the numbers are
				// the half that must survive the ellipsis.
				parts = append(parts, fmt.Sprintf("%s (%d/%d) %s", d, de.Done, de.Total, de.Title))
			}
		}
		b.WriteString(th.muted.Render(wrapJoin(parts, " · ", w)) + "\n")
	}
	stamps := fmt.Sprintf("updated %s · created %s", ago(t.Updated), t.Created.Format("2006-01-02"))
	if !t.Due.IsZero() {
		// Local: the instant furrow stores is UTC, and an evening-local due
		// renders one day early if it is formatted in that zone.
		due := "due " + t.Due.Local().Format("2006-01-02")
		if t.Due.Before(nowFn()) && t.Closed.IsZero() {
			due = th.danger.Render(due + " · OVERDUE")
		} else {
			due = th.warn.Render(due)
		}
		b.WriteString(due + "\n")
	}
	b.WriteString(th.dim.Render(stamps) + "\n")

	// --- dependencies: resolved, bidirectional, never raw ids ---------------
	b.WriteString("\n" + sectionRule(th, "dependencies", w) + "\n")
	b.WriteString(m.depSection(t, w))

	// Refs in furrow's own order (a sequence, not a sorted set), one per line:
	// a file:line or URL is an opaque token to jump to, so it is truncated as a
	// whole rather than wrapped — a path broken across lines cannot be copied.
	if len(t.Refs) > 0 {
		b.WriteString("\n" + sectionRule(th, fmt.Sprintf("refs %d", len(t.Refs)), w) + "\n")
		for _, r := range t.Refs {
			b.WriteString("  " + th.base.Render(ansi.Truncate(r, w-2, "…")) + "\n")
		}
	}

	if len(t.Checklist) > 0 {
		d, tot := t.CheckProgress()
		b.WriteString("\n" + sectionRule(th, fmt.Sprintf("checklist %d/%d", d, tot), w) + "\n")
		for _, c := range t.Checklist {
			mark, st := "[ ]", th.base
			if c.Done {
				mark, st = "[x]", th.dim
			}
			for i, l := range wrapLines(c.Text, w-4) {
				if i == 0 {
					b.WriteString(th.muted.Render(mark) + " " + st.Render(l) + "\n")
				} else {
					b.WriteString("    " + st.Render(l) + "\n")
				}
			}
		}
	}

	if strings.TrimSpace(t.Body) != "" {
		b.WriteString("\n" + sectionRule(th, "body", w) + "\n")
		b.WriteString(renderProse(th, t.Body, w))
	}
	return b.String()
}

// depSection is the resolved bidirectional dep list — the answer to "why can I
// not start this?" and "what am I holding up?", which raw ids cannot give.
//
// The real board measures max 8 rows and mean 2.6 per task, so this always
// fits: no layout engine, no scrolling heuristics, no DAG drawing.
func (m *Model) depSection(t *board.Task, w int) string {
	th := m.th
	var b strings.Builder

	b.WriteString(th.muted.Render("blocked by") + "\n")
	if len(t.Deps) == 0 {
		b.WriteString("  " + th.dim.Render("— nothing") + "\n")
	}
	for _, id := range t.Deps {
		b.WriteString("  " + m.depLine(id, w-2) + "\n")
	}

	rev := m.g.Blocks(t.ID)
	b.WriteString(th.muted.Render("blocks") + "\n")
	if len(rev) == 0 {
		b.WriteString("  " + th.dim.Render("— nothing") + "\n")
	}
	for _, id := range rev {
		b.WriteString("  " + m.depLine(id, w-2) + "\n")
	}

	if m.treeOpen {
		b.WriteString("\n" + sectionRule(th, "transitive (t closes)", w) + "\n")
		b.WriteString(m.depTree(t.ID, board.DirBlockedBy, w) + "\n")
		if len(rev) > 0 {
			b.WriteString(m.depTree(t.ID, board.DirBlocks, w) + "\n")
		}
	}
	return b.String()
}

// depLine resolves one dep id to "<state> <id> — <title> [lane]".
func (m *Model) depLine(id string, w int) string {
	th := m.th
	dep := m.b.Task(id)
	if dep == nil {
		return th.danger.Render(glyphUnknown+" "+id) + th.dim.Render(" — not on this board")
	}
	mark, st := th.warn.Render(glyphOpen), th.base
	if m.g.IsDone(id) {
		mark, st = th.ok.Render(glyphDone), th.dim
	}
	lane := th.dim.Render("[" + dep.Status + "]")
	head := mark + " " + th.chipAlt.Render(id) + " "
	room := w - lg.Width(head) - lg.Width(lane) - 1
	return head + st.Render(pad(dep.Title, maxInt(4, room))) + " " + lane
}

// depTree is the stretch goal: a transitive dep tree via lipgloss/v2/tree.
//
// Two honest limits, both cheap: tree has no multi-parent support, so a node
// reachable twice is DRAWN twice (measured cost on the real board: ~2.5% extra
// rows), and tree styles do not inherit into subtrees, so every label is styled
// as a finished string before it is handed over.
func (m *Model) depTree(id string, dir board.Dir, w int) string {
	root := m.g.TreeOf(id, dir, 4)
	label := "blocked by — what must finish first"
	if dir == board.DirBlocks {
		label = "blocks — what closing this unblocks"
	}
	t := tree.Root(m.th.muted.Render(label))
	for _, c := range root.Children {
		t.Child(m.treeNode(c, w-4)) // the "├── " gutter tree adds per level
	}
	if len(root.Children) == 0 {
		t.Child(m.th.dim.Render("— nothing"))
	}
	return t.String()
}

func (m *Model) treeNode(n *board.DepNode, w int) any {
	suffix := ""
	switch {
	case n.Repeat:
		suffix = " ↩seen"
	case n.Elided:
		suffix = " ⋯"
	}
	label := m.depLine(n.ID, maxInt(10, w-lg.Width(suffix))) + m.th.dim.Render(suffix)
	if len(n.Children) == 0 {
		return label
	}
	sub := tree.Root(label)
	for _, c := range n.Children {
		sub.Child(m.treeNode(c, w-4))
	}
	return sub
}

func sectionRule(th *theme, label string, w int) string {
	head := th.peekHdr.Render(label) + " "
	n := w - lg.Width(head)
	if n < 1 {
		return head
	}
	return head + th.rule.Render(strings.Repeat("─", n))
}

func wrapJoin(parts []string, sep string, w int) string {
	var lines []string
	cur := ""
	for _, p := range parts {
		// Wrapping happens at part boundaries only, so one over-wide part (a
		// resolved epic title) would push the whole block past the box border.
		if lg.Width(p) > w {
			p = ansi.Truncate(p, w, "…")
		}
		cand := p
		if cur != "" {
			cand = cur + sep + p
		}
		if lg.Width(cand) > w && cur != "" {
			lines = append(lines, cur)
			cur = p
			continue
		}
		cur = cand
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return strings.Join(lines, "\n")
}

// renderProse is a deliberately tiny markdown styler. glamour is available but
// this POC keeps the body deterministic (--dump has to be diffable) and avoids
// a second wrapping engine disagreeing with lipgloss about CJK widths.
func renderProse(th *theme, body string, w int) string {
	var b strings.Builder
	fence := false
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimRight(raw, " ")
		if strings.HasPrefix(strings.TrimSpace(line), "```") {
			fence = !fence
			b.WriteString(th.dim.Render(pad(line, w)) + "\n")
			continue
		}
		if fence {
			b.WriteString(th.dim.Render(pad(line, w)) + "\n")
			continue
		}
		trimmed := strings.TrimSpace(line)
		switch {
		case trimmed == "":
			b.WriteString("\n")
		case strings.HasPrefix(trimmed, "#"):
			for _, l := range wrapLines(strings.TrimLeft(trimmed, "# "), w) {
				b.WriteString(th.peekHdr.Render(l) + "\n")
			}
		case strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* "):
			for i, l := range wrapLines(trimmed[2:], w-2) {
				lead := th.muted.Render("• ")
				if i > 0 {
					lead = "  "
				}
				b.WriteString(lead + th.base.Render(l) + "\n")
			}
		default:
			for _, l := range wrapLines(trimmed, w) {
				b.WriteString(th.base.Render(l) + "\n")
			}
		}
	}
	return b.String()
}

// ago renders a coarse relative time; the exact stamp is a `furrow show` away.
func ago(t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := nowFn().Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 90*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.Format("2006-01-02")
}

package ui

import (
	"fmt"
	"strings"

	"github.com/akira-toriyama/ridge/internal/board"
)

// One vocabulary for a box's facts, for the four surfaces that state them.
//
// The dep edges are classified at three of those — the peek's `epic waits on`,
// the box overview's strip, the epic overlay's deps list — and the head line
// and meta chips at two (the strip again, and the slice panel's readout).
// Three of those five call sites carried a comment forbidding a second
// vocabulary and the strip's head/meta carried none, so the invariant was held
// by prose that did not even cover it: the three dep ladders had already
// drifted to six, five and four branches.
//
// What is shared is the CLASSIFICATION, not the rendering. The glyph prefix,
// the STUCK marker and whether a resolved dep shows its numbers are deliberate
// per-surface choices, so they stay at the call sites. So does the GATE: the
// peek renders its line only while furrow's open_deps is non-empty, the strip
// renders over Deps whether or not any is still open. This file classifies one
// edge it is handed and decides nothing about whether to draw a line at all.
//
// WHY THESE WORDS. furrow's `epic dep --list` prints [open], [closed] and [?],
// and over the --all read all three are decidable from what the board holds —
// measured on v5.0.0 with one box carrying all three at once, open_deps was
// exactly the OPEN dep, excluding both the closed and the dangling one. So a
// dep the board holds as closed renders (closed), and one it cannot resolve at
// all renders (missing) — furrow's lint code is epic-dep-missing, at severity
// ERROR. Calling that second one "satisfied" would put a reassuring word on a
// broken reference.
//
// (satisfied) survives for the case the measurement says cannot happen: furrow
// settled a dep whose box this board still shows OPEN. No board furrow
// produces has one, so it has no fixture site, and it says the weakest true
// thing rather than inventing a reason.
//
// Two more readings of the same edge stay out. boxDepTag (boxboardview.go)
// reads Closed without OpenDeps, because an inline row tag answers "is that
// box done" rather than "does this box still wait". And the peek's own-epic
// line (peek.go) composes the same id/progress/title with STUCK as a SUFFIX
// rather than an infix — it is describing the task's own box, not an edge.

// epicDepState is one dep edge as the surfaces above distinguish it.
type epicDepState int

const (
	epicDepUnresolved epicDepState = iota // open per furrow, absent from this read
	epicDepStuck                          // open, and furrow calls the box stuck
	epicDepOpen                           // open
	epicDepMissing                        // not open, and unresolvable — furrow's [?]
	epicDepClosed                         // not open, and the board holds it closed
	epicDepSatisfied                      // not open, resolvable, and not closed
)

// epicDepStateOf classifies one dep edge. stillOpen is membership of the
// owning box's OpenDeps — furrow's verdict, never recomputed here. de is the
// dep's own box, nil when this read cannot resolve it; every arm that reads
// de branches on nil first.
func epicDepStateOf(de *board.EpicInfo, stillOpen bool) epicDepState {
	if stillOpen {
		switch {
		case de == nil:
			return epicDepUnresolved
		case de.Stuck:
			return epicDepStuck
		}
		return epicDepOpen
	}
	switch {
	case de == nil:
		return epicDepMissing
	case !de.Closed.IsZero():
		return epicDepClosed
	}
	return epicDepSatisfied
}

// epicDepLabel is `id (done/total) title`, the composition five of the six dep
// sites share. Progress BEFORE the title: a CJK box title routinely overflows
// and truncates, and the numbers are the half that must survive the ellipsis.
// An unresolvable dep degrades to the bare id rather than claiming 0/0.
//
// mark rides BETWEEN the numbers and the title when non-empty. Only the peek's
// DEP line passes one; its own-epic line spells the same facts with STUCK as a
// suffix and does not come through here.
func epicDepLabel(id string, de *board.EpicInfo, mark string) string {
	if de == nil {
		return id
	}
	if mark != "" {
		return fmt.Sprintf("%s (%d/%d) %s %s", id, de.Done, de.Total, mark, de.Title)
	}
	return fmt.Sprintf("%s (%d/%d) %s", id, de.Done, de.Total, de.Title)
}

// boxHead is a box's first line: the id chip, the title, and the closing date
// beside it when it has one.
func (m *Model) boxHead(e *board.EpicInfo) string {
	head := m.th.chipAlt.Render(e.ID) + " " + m.th.base.Render(e.Title)
	if !e.Closed.IsZero() {
		head += m.th.dim.Render("  closed " + e.Closed.In(board.Zone()).Format("2006-01-02"))
	}
	return head
}

// boxMeta is furrow's own words for a box, in the order both surfaces state
// them. Returned unjoined: the box strip wraps them with wrapJoin against the
// panel's width, the slice readout is a single unwrapped status line, and
// joining here would start wrapping that row.
//
// full is the box strip, which has the panel width for the box's whole record.
// The slice readout is one line, so it drops labels and meta and carries
// `waits on N` instead — the strip resolves those ids on a line of its own.
func (m *Model) boxMeta(e *board.EpicInfo, full bool) []string {
	th := m.th
	meta := []string{fmt.Sprintf("%d/%d done", e.Done, e.Total)}
	if e.Active {
		meta = append(meta, th.ok.Render("active"))
	}
	if e.Standing {
		meta = append(meta, "standing")
	}
	if e.Pinned {
		meta = append(meta, "pinned")
	}
	if e.Stuck {
		meta = append(meta, th.warn.Render("STUCK"))
	}
	if !full {
		if n := len(e.OpenDeps); n > 0 {
			meta = append(meta, fmt.Sprintf("waits on %d", n))
		}
	}
	if len(e.Repos) > 0 {
		// The one field that separates the reserved boxes: 99 of the real
		// board's 133 open boxes are titled mandate / parking-lot / requests,
		// one per repo (measured 2026-09-11), so for three quarters of the
		// epic axis the repo is the only thing that tells two rows apart.
		meta = append(meta, "repos "+strings.Join(e.Repos, ","))
	}
	if full {
		if len(e.Labels) > 0 {
			meta = append(meta, "labels "+strings.Join(e.Labels, ","))
		}
		if keys := e.MetaKeys(); len(keys) > 0 {
			meta = append(meta, "meta "+strings.Join(keys, ","))
		}
	}
	return meta
}

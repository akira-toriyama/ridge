package ui

import (
	"fmt"
	"strings"

	"github.com/akira-toriyama/ridge/internal/board"
)

// Geometry shared by the FULL-SCREEN views (graph, dep map, box overview,
// roadmap). All are laid out as title bar / header / drawing / strip / status,
// so the numbers that separate those bands belong to none of them.
const (
	fullTop = 2 // title bar + header: the first row the drawing may use
	stripH  = 8 // the detail strip, border included
)

// fullScreen reports whether a view that OWNS the whole terminal is up. The
// board's own chrome — the peek and the slice panel — composites nothing in
// these views, so every guard that means "the board's chrome is not on screen"
// asks this rather than naming one of them. Naming just the graph is how a
// reopened modal ended up holding the keyboard while rendering nowhere.
//
// It does NOT mean "modals composite nothing". The box overview draws
// modalLayers, because it is the one full-screen view with a key that opens
// one; the two guards that still refuse to REOPEN a refused modal here
// (addmode.go, epicmode.go) are conservative rather than required, since
// neither modal can be submitted from that view in the first place.
func (m *Model) fullScreen() bool {
	return m.view == viewGraph || m.view == viewMap || m.view == viewBoxes ||
		m.view == viewRoadmap || m.view == viewSwim || m.view == viewSweep
}

// stripHeight shrinks the detail strip on a short terminal rather than letting
// it push the drawing off the screen entirely.
func (m *Model) stripHeight() int {
	if m.h < 24 {
		return minInt(stripH, maxInt(0, m.h-fullTop-footerH-3))
	}
	return stripH
}

// The detail strip: the selected task's full record, uncut, pinned under a
// full-screen view.
//
// The layout research flagged LABELLING as the hard half of any dependency
// picture, and it is right: a box or a row can only ever show a truncated
// title, so the view would be a picture you cannot read the captions of. The
// strip is the answer — the selection's whole title and metadata, wrapped,
// never elided, always on screen, so the drawing above it is free to be a MAP
// rather than a document.
//
// The task-rowed full-screen views share it: the graph's node boxes, the
// dependency map's rows and the roadmap's pane have exactly the same problem
// and must not answer it three times.
func (m *Model) taskStrip(t *board.Task, hidden bool, h int) string {
	th := m.th
	inner := maxInt(10, m.w-4)
	if t == nil {
		return th.peek.Width(m.w).Height(h).Render(th.dim.Render("no node selected"))
	}

	// Two columns when there is room: identity and prose on the left, the
	// resolved dep lists on the right. At 240+ this is free real estate.
	leftW := inner
	rightW := 0
	if inner >= 120 {
		rightW = inner / 2
		leftW = inner - rightW - 3
	}

	var left []string
	head := th.peekHdr.Render(t.ID) + " " + th.chipAlt.Render("["+t.Status+"]")
	if m.g.Actionable(t.ID) {
		head += " " + th.ok.Render(glyphActionable+" actionable")
	}
	if nb := len(m.g.OpenBlockedBy(t.ID)); nb > 0 {
		head += " " + th.danger.Render(fmt.Sprintf("%s blocked by %d", glyphBlocked, nb))
	}
	if hidden {
		head += " " + th.warn.Render("· hidden by the current filter")
	}
	left = append(left, head)
	// The FULL title, wrapped, never truncated — that is the strip's whole job.
	for _, line := range wrapLines(t.Title, leftW) {
		left = append(left, th.base.Render(line))
	}
	var meta []string
	if t.Value > 0 || t.Effort > 0 {
		meta = append(meta, fmt.Sprintf("value %d", t.Value), fmt.Sprintf("effort %d", t.Effort))
	}
	if len(t.Repos) > 0 {
		meta = append(meta, "repos "+strings.Join(t.Repos, ","))
	} else {
		meta = append(meta, "draft (no repo)")
	}
	if len(t.Labels) > 0 {
		meta = append(meta, "labels "+strings.Join(t.Labels, ","))
	}
	if t.Epic != "" {
		// Resolve the way the card, the table column and the peek all do. An
		// epic is an entity; the raw e- id in a frame is a leak the repo
		// already asserts against in two other views.
		label := t.Epic
		if e := m.b.Epic(t.Epic); e != nil {
			label = e.Title
		}
		meta = append(meta, "epic "+label)
	}
	if d, tot := t.CheckProgress(); tot > 0 {
		meta = append(meta, fmt.Sprintf("checklist %d/%d", d, tot))
	}
	meta = append(meta, "updated "+ago(t.Updated))
	for _, line := range strings.Split(wrapJoin(meta, " · ", leftW), "\n") {
		left = append(left, th.muted.Render(line))
	}

	var right []string
	if rightW > 0 {
		up, down := m.g.BlockedBy(t.ID), m.g.OpenBlocks(t.ID)
		right = append(right, th.muted.Render(fmt.Sprintf("blocked by %d open · blocks %d open",
			len(up), len(down))))
		for _, id := range t.Deps {
			right = append(right, "↑ "+m.depLine(id, rightW-2))
		}
		for _, id := range m.g.Blocks(t.ID) {
			right = append(right, "↓ "+m.depLine(id, rightW-2))
		}
		if len(t.Deps) == 0 && len(m.g.Blocks(t.ID)) == 0 {
			right = append(right, th.dim.Render("— no dependency edges —"))
		}
	}

	// A strip too short to hold a single content row renders nothing rather
	// than panicking: stripHeight lands on exactly 1 at m.h==7, and
	// `make([]string, 0, h-2)` with h==1 is a negative capacity
	// (`-dump -demo map -rows 7` reaches it).
	body := maxInt(0, h-2)
	rows := make([]string, 0, body)
	for i := 0; i < body; i++ {
		lseg, rseg := "", ""
		if i < len(left) {
			lseg = left[i]
		}
		if i < len(right) {
			rseg = right[i]
		}
		if rightW == 0 {
			rows = append(rows, pad(lseg, inner))
			continue
		}
		rows = append(rows, pad(lseg, leftW)+"   "+pad(rseg, rightW))
	}
	return th.peek.Width(m.w).Height(h).Render(strings.Join(rows, "\n"))
}

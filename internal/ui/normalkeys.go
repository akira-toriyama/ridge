package ui

import (
	"strings"

	"charm.land/bubbles/v2/key"
	tea "charm.land/bubbletea/v2"
)

// onNormalKey is the board's and the table's key surface: every gesture that
// is not owned by a modal, a full-screen view, or move mode lands here, and
// keys.go's HelpSections "normal mode" block is the list of what it answers.

func (m *Model) onNormalKey(msg tea.KeyPressMsg) tea.Cmd {
	switch {
	case key.Matches(msg, m.keys.Quit):
		return m.quitOrFlush()

	case key.Matches(msg, m.keys.Help):
		m.fullHelp = !m.fullHelp

	case key.Matches(msg, m.keys.Cancel):
		switch {
		// The help overlay sits at zHelp, above every other layer, so it is
		// what Esc must take off first. Without this case Esc reached past the
		// overlay the user was looking at and closed the peek UNDER it — the
		// board looked frozen and lost state at the same time. The graph's
		// Cancel has always had this branch; only the board's was missing.
		case m.fullHelp:
			m.fullHelp = false
		case m.treeOpen:
			m.treeOpen = false
		case m.peekOpen:
			m.peekOpen = false
		case m.qRaw != "":
			cmd := m.applyFilter("")
			m.ti.SetValue("")
			m.note("filter cleared")
			return cmd
		case m.sliceVal != "":
			// A slice-only filter must be escapable too. Radio semantics:
			// re-selecting clears.
			return m.selectSlice(m.sliceField, m.sliceVal)
		}

	case key.Matches(msg, m.keys.Filter):
		// Entering a modal input mid-drag would leave the drag armed and its
		// release would commit a move nobody is looking at any more.
		m.cancelDrag()
		// A modal owns the whole keyboard, so the help overlay must not ride
		// into it: `?` cannot be typed there to close it, and its "you are
		// here" would keep naming the mode that was just left.
		m.fullHelp = false
		m.mode = modeFilter
		m.ti.SetValue(m.qRaw)
		return m.ti.Focus()

	case key.Matches(msg, m.keys.View):
		// No note: the tab strip in the title row already says which view is
		// up, and the whole screen changed.
		// Capture the selection BEFORE switching: curTask() reads whichever
		// view is current, so asking after the switch returned table row 0 —
		// the row just assigned — and selectID then wrote that back over the
		// board cursor, lane included. setSort already does this correctly
		// ("a sort that teleports the cursor ... is a re-ordering of the user").
		t := m.curTask()
		if m.view == viewBoard {
			m.view, m.tableIdx = viewTable, 0
		} else {
			m.view = viewBoard
		}
		if t != nil {
			m.selectID(t.ID, false)
		}

	case key.Matches(msg, m.keys.Sort):
		if m.view != viewTable {
			m.note("sort belongs to the table — v switches views")
			return nil
		}
		m.cycleSort()

	case key.Matches(msg, m.keys.ViewTab):
		return m.switchView(viewTabDigit(msg))

	case key.Matches(msg, m.keys.ViewSave):
		m.saveView()

	case key.Matches(msg, m.keys.Mouse):
		m.mouseOn = !m.mouseOn
		if m.mouseOn {
			m.note("mouse tracking ON — drag cards; the terminal's own text selection needs the bypass modifier")
		} else {
			m.note("mouse tracking OFF — the terminal owns the mouse again; keyboard still does everything")
		}

	case key.Matches(msg, m.keys.Graph):
		m.openGraph()

	case key.Matches(msg, m.keys.Map):
		id := ""
		if t := m.curTask(); t != nil {
			id = t.ID
		}
		m.openMap(id)

	case key.Matches(msg, m.keys.Boxes):
		m.openBoxes()

	case key.Matches(msg, m.keys.Sweep):
		return m.openSweep()

	case key.Matches(msg, m.keys.Roadmap):
		m.openRoadmap()

	case key.Matches(msg, m.keys.Swim):
		m.openSwim()

	case key.Matches(msg, m.keys.Peek):
		m.peekOpen = !m.peekOpen
		if !m.peekOpen {
			// The tree renders only INSIDE the peek, so peek=false/tree=true is
			// invisible state: the next esc came off the ladder's treeOpen rung
			// and changed no pixel, and the next `t` appeared dead because it
			// was toggling the tree back OFF. Make the pair unrepresentable
			// rather than leave a state no frame can show.
			m.treeOpen = false
		}
		m.syncPeek()

	case key.Matches(msg, m.keys.PeekScroll):
		// Driven explicitly rather than by forwarding keys to the viewport:
		// viewport's default keymap owns up/down, which would make j/k scroll
		// the peek AND move the board cursor at the same time.
		//
		// The keys scroll whatever the user is looking at — the open peek
		// wins, then the table, then the focused column; the help advertises
		// a plain "scroll", so a gesture that cannot move must say so rather
		// than sit as a silent dead key.
		down := msg.String() == "ctrl+d"
		dir := 1
		if !down {
			dir = -1
		}
		switch {
		case m.peekOpen:
			if down {
				m.vp.ScrollDown(maxInt(1, m.vp.Height()/2))
			} else {
				m.vp.ScrollUp(maxInt(1, m.vp.Height()/2))
			}
		case m.view == viewTable:
			// The table's window centers on the cursor, so half a page of
			// rows IS a cursor move — j/k's model, just bigger.
			rows := len(m.tableRows())
			step := maxInt(1, maxInt(1, m.tableVisRows())/2)
			before := m.tableIdx
			m.tableIdx = clamp(m.tableIdx+dir*step, 0, maxInt(0, rows-1))
			if m.tableIdx == before {
				m.note("already at the %s of the table", endName(dir))
			}
		default:
			// The focused board column, in the wheel's unit (cards) with the
			// wheel's guards, so the two scroll surfaces cannot disagree
			// about where the column ends. The cursor deliberately stays put
			// — like the wheel, a column scrolled away from its cursor is a
			// legitimate state (the next arrow re-pulls).
			lane := m.curLaneName()
			moved := 0
			if c := m.lay.Col(lane); c != nil {
				if len(c.Tasks) == 0 {
					m.note("%s is empty", lane)
					return nil
				}
				for i := maxInt(1, len(c.Cards)/2); i > 0; i-- {
					cc := m.lay.Col(lane)
					if down && cc.Hidden > 0 {
						m.scroll[lane] = cc.Scroll + 1
					} else if !down && cc.Scroll > 0 {
						m.scroll[lane] = cc.Scroll - 1
					} else {
						break
					}
					m.relayout()
					moved++
				}
			}
			if moved == 0 {
				m.note("%s is already at the %s", lane, endName(dir))
			}
		}

	case key.Matches(msg, m.keys.Tree):
		m.treeOpen = !m.treeOpen
		if m.treeOpen {
			m.peekOpen = true
		}
		m.syncPeek()

	case key.Matches(msg, m.keys.OnlyBlock):
		// TOKEN-wise, never a substring ReplaceAll: on "-is:blocked" that left a
		// bare "-" behind, which parses as a bare-word term and quietly changed
		// what the board showed while claiming the toggle was off.
		//
		// No note either way: `b` edits the filter query itself, so the filter
		// bar shows `is:blocked` appearing and disappearing.
		if q, had := dropToken(m.qRaw, "is:blocked"); had {
			cmd := m.applyFilter(q)
			m.ti.SetValue(m.qRaw)
			return cmd
		}
		cmd := m.applyFilter(strings.TrimSpace(m.qRaw + " is:blocked"))
		m.ti.SetValue(m.qRaw)
		return cmd

	case key.Matches(msg, m.keys.Revisit):
		return m.toggleRevisit()

	case key.Matches(msg, m.keys.JumpBlock):
		m.jumpToBlocker()
	case key.Matches(msg, m.keys.JumpBack):
		m.jumpBack()

	case key.Matches(msg, m.keys.Reload):
		if m.queueBusy() {
			// The reload would race the queue's own furrow process, land
			// behind the guard in onReloadDone and be dropped — leaving
			// "reloading…" on screen forever. The drain reconciles anyway.
			m.note("writes in flight — the board re-reads itself once they land")
			return nil
		}
		label := "reloaded"
		if !m.prov.Live() {
			label = "reloaded from the fixture — session edits discarded"
		}
		m.note("reloading…")
		return m.reloadCmd(label)

	case key.Matches(msg, m.keys.Sync):
		if !m.prov.Live() {
			m.note("the fixture has no store to sync")
			return nil
		}
		if m.queueBusy() {
			m.note("writes in flight — sync once they land")
			return nil
		}
		m.note("syncing…")
		return m.syncCmd()

	case key.Matches(msg, m.keys.Add):
		m.fullHelp = false // same rule as Filter: a modal never inherits the overlay
		return m.enterAdd()

	case key.Matches(msg, m.keys.Slice):
		m.fullHelp = false
		m.toggleSlice()

	case key.Matches(msg, m.keys.Done):
		if t := m.curTask(); t != nil {
			id := t.ID
			// Board.Close moves the card out of its lane before enqueuePersist
			// can refuse, and that jump is what survives the window (t-8nyd's
			// shape, on the close path).
			if m.refuseWhileRollingBack("done " + id) {
				return nil
			}
			unblocked := len(m.g.OpenBlocks(id))
			if err := m.b.Close(id); err != nil {
				m.fail("%v", err)
				return nil
			}
			m.recompute()
			if unblocked > 0 {
				m.note("closed %s — unblocked %d task(s)", id, unblocked)
			} else {
				m.note("closed %s", id)
			}
			return m.enqueuePersist("done "+id, func() ([]string, error) {
				return nil, m.prov.PersistDone(id)
			})
		}

	case key.Matches(msg, m.keys.Edit):
		if t := m.curTask(); t != nil {
			return m.editCmd(t)
		}

	case key.Matches(msg, m.keys.Review):
		return m.reviewCmd()

	case key.Matches(msg, m.keys.Note):
		m.fullHelp = false // same rule as Filter: a modal never inherits the overlay
		return m.enterNote()

	case key.Matches(msg, m.keys.Move):
		// GitHub's Enter is cell EDITING; move mode is the board-only lift.
		// With the peek open (or on a table row) Enter edits the fields — the
		// board without a peek keeps Enter as the move-mode muscle memory.
		//
		// Close the overlay either way: the edit modal never inherits it, and
		// a lift needs the board visible — `?` works inside move mode for
		// whoever wants the listing back.
		m.fullHelp = false
		if m.view == viewTable || m.peekOpen {
			m.enterEdit()
		} else {
			m.enterMove()
		}

	case key.Matches(msg, m.keys.QuickUp):
		return m.quickReorder(-1)
	case key.Matches(msg, m.keys.QuickDown):
		return m.quickReorder(+1)

	case key.Matches(msg, m.keys.LaneBack):
		return m.cycleLane(-1)
	case key.Matches(msg, m.keys.LaneFwd):
		return m.cycleLane(+1)

	case key.Matches(msg, m.keys.Top):
		m.setPos(0)
	case key.Matches(msg, m.keys.Bottom):
		m.setPos(len(m.curTasks()) - 1)

	case key.Matches(msg, m.keys.Up):
		m.moveCursor(0, -1)
	case key.Matches(msg, m.keys.Down):
		m.moveCursor(0, +1)
	case key.Matches(msg, m.keys.Left):
		m.moveCursor(-1, 0)
	case key.Matches(msg, m.keys.Right):
		m.moveCursor(+1, 0)
	case key.Matches(msg, m.keys.NextCol):
		m.moveCursor(+1, 0)
	case key.Matches(msg, m.keys.PrevCol):
		m.moveCursor(-1, 0)
	}
	return nil
}

// dropToken removes every occurrence of `tok` from a whitespace-separated query,
// reporting whether it was there.
func dropToken(raw, tok string) (string, bool) {
	var keep []string
	had := false
	for _, f := range strings.Fields(raw) {
		if f == tok {
			had = true
			continue
		}
		keep = append(keep, f)
	}
	return strings.Join(keep, " "), had
}

// reviewCmd is the `i` key: stamp the selected task reviewed. Optimistic like
// done — Board.Review sets the clock, `furrow review` records it — and the
// note is the only visible change on the board itself: the peek's stamps line
// is where the clock shows.
func (m *Model) reviewCmd() tea.Cmd {
	t := m.curTask()
	if t == nil {
		m.note("nothing selected — a review stamps a task")
		return nil
	}
	id := t.ID
	if m.refuseWhileRollingBack("review " + id) {
		return nil
	}
	if err := m.b.Review(id); err != nil {
		m.fail("%v", err)
		return nil
	}
	m.syncPeek()
	m.note("reviewed %s", id)
	return m.enqueuePersist("review "+id, func() ([]string, error) {
		return nil, m.prov.PersistReview(id)
	})
}

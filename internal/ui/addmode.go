package ui

import (
	"strconv"
	"strings"

	"charm.land/bubbles/v2/key"
	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/akira-toriyama/ridge/internal/board"
)

// chipTrunc bounds one chip's free text (a checklist item, a ref) so a long
// paste cannot balloon the modal. Cell-width truncation, never bytes — the
// text is CJK prose.
func chipTrunc(s string) string {
	return ansi.Truncate(s, 24, "…")
}

// chipWrap lays chips into lines of at most w display cells, breaking only
// BETWEEN chips. A chip longer than a whole line still gets one to itself
// (pathological, and clipping it is the caller's box's problem, not ours).
func chipWrap(prefix string, chips []string, w int) []string {
	if len(chips) == 0 {
		return nil
	}
	indent := strings.Repeat(" ", lg.Width(prefix))
	var lines []string
	cur := prefix + chips[0]
	for _, c := range chips[1:] {
		joined := cur + " · " + c
		if lg.Width(joined) > w {
			lines = append(lines, cur)
			cur = indent + c
			continue
		}
		cur = joined
	}
	return append(lines, cur)
}

// Quick add (t-m89b): GitHub Projects' "+ Add item". One text input, one
// Enter — and the ACTIVE FILTER'S metadata is inherited by the new task, the
// GH rule that makes adding from a filtered view mean "add into what I am
// looking at". Inherited values are shown as chips in the modal, never
// applied silently. Details ride the same line as inline tokens (addparse.go,
// t-69v9), echoed live in the same chips row; the edit overlay remains the
// after-the-fact route. The modal stays one line either way.

type addState struct {
	input textinput.Model
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
	// effectiveQuery, not qRaw: the slice term IS part of the applied filter
	// (glossary), so slicing to epic:e-x and adding must stamp e-x exactly as
	// typing the same term would — the chips row shows whatever is stamped.
	o.Label, o.Epic, o.Repo, o.Draft = inheritContext(m.effectiveQuery())
	m.add = &addState{input: ti, opts: o}
	m.mode = modeAdd
	return m.add.input.Focus()
}

// inheritContext lifts the single-valued, un-negated label:/epic:/repo:
// tokens out of the active query — exactly the values GH would stamp onto an
// item added under that filter — plus is:draft, the one is: state an add can
// stamp: under a draft view a plain add would be born repo-attached and
// vanish from the very view it was added into. Comma'd (OR) and negated
// tokens inherit nothing: "one of these" is not a value to stamp.
func inheritContext(raw string) (label, epic, repo string, draft bool) {
	for _, tok := range strings.Fields(raw) {
		k, v, ok := strings.Cut(tok, ":")
		// Quoted values (label:"needs review", either quote character) split
		// across Fields tokens; like OR'd and negated tokens they inherit
		// nothing rather than inheriting a mangled fragment.
		if !ok || v == "" || strings.HasPrefix(k, "-") ||
			strings.Contains(v, ",") || strings.ContainsAny(v, `"'`) {
			continue
		}
		switch strings.ToLower(k) {
		case "label":
			label = v
		case "epic":
			epic = v
		case "repo":
			repo = v
		case "is":
			// EqualFold: furrow's -q matches the value case-insensitively
			// (measured — is:DRAFT answers the same rows), so a case-exact
			// check here would narrow the view and then silently repo-attach
			// the add (found by review).
			if strings.EqualFold(v, "draft") {
				draft = true
			}
		}
	}
	return label, epic, repo, draft
}

func (m *Model) onAddKey(msg tea.KeyPressMsg) tea.Cmd {
	a := m.add
	switch {
	case key.Matches(msg, m.keys.ForceQuit):
		// The modal owns the keyboard, so `q` types — but ctrl+c must always
		// be a way out (bubbletea raw mode delivers it as a plain keystroke).
		return m.quitOrFlush()
	case key.Matches(msg, m.keys.Cancel):
		m.mode, m.add = modeNormal, nil
		return nil
	case key.Matches(msg, m.keys.Commit):
		raw := a.input.Value()
		title, tk := parseAddLine(raw)
		if len(tk.bad) > 0 {
			// Refuse in-modal, naming the first offender — the same
			// keep-the-typed-line contract as the empty title. Checked
			// FIRST: a tokens-only line is a token problem, and "a title
			// cannot be empty" would name the wrong cause (found by review).
			m.fail("%s", tk.bad[0])
			return nil
		}
		if title == "" {
			m.fail("a title cannot be empty")
			return nil
		}
		opts := tk.apply(a.opts)
		if err := opts.Validate(); err != nil {
			// The semantic half (estimate range, due grammar, ref CSV):
			// refused here so the line survives instead of costing a store
			// round trip that would bounce anyway.
			m.fail("%v", err)
			return nil
		}
		if m.rollingBack {
			// The store refused the last write and the board is rolling
			// back; every write path waits for the re-read. Keep the modal
			// open — the typed title must survive the refusal.
			m.fail("the store refused the last write — rolling back; press ⏎ again in a moment")
			return nil
		}
		m.mode, m.add = modeNormal, nil
		m.note("adding…")
		return m.enqueueAdd(title, raw, a.opts, opts)
	}
	var c tea.Cmd
	a.input, c = a.input.Update(msg)
	return c
}

// reopenRefusedAdd hands a refused add's typed title back by reopening the
// modal — but only from modeNormal. The refusal lands ~100ms after the
// gesture, and the user may already be typing a SECOND draft, lifting a
// card, or filtering; stealing that mode would clobber the newer work, so
// anywhere else the title survives only in the failure note (whose label
// names it).
func (m *Model) reopenRefusedAdd(op persistOp) tea.Cmd {
	if m.mode != modeNormal || m.view == viewGraph || m.fullHelp {
		// The graph shares modeNormal but never composites addLayer — a
		// reopen there would put the keyboard inside an invisible modal
		// (t-74y3). The help overlay hides addLayer the same way (zHelp is
		// above it), and `?` would TYPE into the reopened input instead of
		// closing what covers it. The failure note still names the title.
		return nil
	}
	ti := textinput.New()
	ti.Prompt = "+ "
	ti.Placeholder = "task title"
	ti.SetWidth(56)
	// The RAW line, tokens and all — a due form furrow refused must come back
	// editable, not silently shorn down to the parsed title. op.addOpts is
	// the inherited half only, so the tokens live in this line ALONE and
	// deleting one really clears it.
	ti.SetValue(op.addRaw)
	m.add = &addState{input: ti, opts: op.addOpts}
	m.mode = modeAdd
	return m.add.input.Focus()
}

// addLayer draws the quick-add modal: the input, the inherited-context chips,
// and the inline tokens echoed back as chips of their own — parsed live on
// every keystroke, so nothing is stamped silently and a typo shows up before
// Enter does. a.opts is the INHERITED context only — enqueueAdd stores the
// pre-apply half for exactly this reason — so the detail fields all come from
// the live parse; reading composed opts here would double every chip on a
// reopened refusal, and make its Draft unclearable.
func (m *Model) addLayer() *lg.Layer {
	th := m.th
	a := m.add
	inner := clamp(m.w/3, 44, 72)

	_, tk := parseAddLine(a.input.Value())

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
	// The repo slot: an inherited repo, or the draft declaration (either
	// source — the filter or the typed is:draft), or the board's auto-attach.
	// A repo AND a draft at once is the conflict Validate refuses; the chip
	// keeps showing the repo and the ⚠ row below names the clash before ⏎.
	draft := a.opts.Draft || tk.draft
	switch {
	case a.opts.Repo != "":
		chips = append(chips, "repo "+a.opts.Repo)
	case draft:
		chips = append(chips, "draft (no repo)")
	default:
		chips = append(chips, "repo (board auto)")
	}
	if tk.value != 0 {
		chips = append(chips, "value "+strconv.Itoa(tk.value))
	}
	if tk.effort != 0 {
		chips = append(chips, "effort "+strconv.Itoa(tk.effort))
	}
	if tk.due != "" {
		chips = append(chips, "due "+chipTrunc(tk.due))
	}
	for _, d := range tk.deps {
		chips = append(chips, "dep "+chipTrunc(d))
	}
	for _, c := range tk.checks {
		chips = append(chips, "check "+chipTrunc(c))
	}
	for _, r := range tk.refs {
		chips = append(chips, "ref "+chipTrunc(r))
	}

	// Wrapped BETWEEN chips, never padded or clipped: seven chips of CJK
	// prose overflow one line, MaxWidth clipping would silently eat exactly
	// the chips this row exists to show, and lipgloss's own word wrap breaks
	// inside a chip ("dep\nt-x" reads as two chips).
	bad := tk.bad
	if draft && a.opts.Repo != "" {
		// The guidance names an act the user can actually perform: a typed
		// token can be dropped, but an inherited draft has no token in the
		// line — only the filter can clear it (found by review).
		if tk.draft {
			bad = append(bad, "is:draft conflicts with repo "+a.opts.Repo+" — drop it, or clear repo: from the filter")
		} else {
			bad = append(bad, "the filter inherits both is:draft and repo "+a.opts.Repo+" — clear one from the filter")
		}
	}

	var rows []string
	for _, l := range chipWrap("→ ", chips, inner) {
		rows = append(rows, th.accent.Render(l))
	}
	for _, l := range chipWrap("⚠ ", bad, inner) {
		rows = append(rows, th.danger.Render(l))
	}

	box := th.peek.Render(
		pad(th.peekHdr.Render("add item"), inner) + "\n\n" +
			a.input.View() + "\n\n" +
			strings.Join(rows, "\n") + "\n" +
			th.dim.Render(pad("⏎ create · esc cancel · ^c quit · more via the edit menu", inner)) + "\n" +
			th.dim.Render(pad("inline: value:4 effort:2 due:+1d dep:t-x check:\"…\" ref:… is:draft", inner)))
	box = lg.NewStyle().MaxWidth(m.w).MaxHeight(m.h).Render(box)
	x := maxInt(0, (m.w-lg.Width(box))/2)
	y := maxInt(0, (m.h-lg.Height(box))/3)
	return lg.NewLayer(box).ID("add").X(x).Y(y).Z(zEdit)
}

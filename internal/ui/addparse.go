package ui

import (
	"strconv"
	"strings"

	"github.com/akira-toriyama/ridge/internal/board"
)

// Quick add's inline tokens (t-69v9): the details `furrow add` takes as flags,
// typed in the modal's one line with the -q filter vocabulary where the two
// overlap (value:4 due:+1d). One line stays the modal's shape — a second stage
// per field would make six fields cost six prompts.
//
//	盤面から起票 value:4 effort:2 due:+1d dep:t-x check:"再現手順を書く" ref:ui/addmode.go
//
// Splitting is on spaces; a `"` or `'` opens a run in which spaces do not
// split, which is how a checklist item carries prose. A field that OPENS with
// a quote is always title text — the escape hatch for a title that itself
// contains `value:`. Unknown keys (and anything after the first `:` of a
// known one) stay verbatim, so `ref:ui/addmode.go:42` and a bare URL both
// survive. label:/epic:/repo:/status:/lane: are deliberately refused rather
// than silently titled: those are the INHERITED half (filter first), and a
// user typing them expects them to land.

// addTokens is one parse of the modal line. bad collects the tokens that
// cannot commit, each with the reason the refusal note will quote — the parse
// itself never fails, because the chips row renders it live on every
// keystroke, mid-word and all.
type addTokens struct {
	value, effort int // 0 = absent; range is Validate's (board-side)
	due           string
	deps          []string
	checks        []string
	refs          []string
	bad           []string // "value:abc — not a number"
}

// apply copies the parsed details onto an inherited-context AddOptions. The
// two halves are disjoint fields, so there is no override rule to invent.
func (tk addTokens) apply(o board.AddOptions) board.AddOptions {
	o.Value, o.Effort, o.Due = tk.value, tk.effort, tk.due
	o.Deps, o.Checks, o.Refs = tk.deps, tk.checks, tk.refs
	return o
}

// addField is one space-delimited field of the modal line, quotes already
// stripped. quoted marks a field that OPENED with a quote — literal title
// text, never a token.
type addField struct {
	text   string
	quoted bool
}

// splitAddFields splits the line the way a shell would with only quoting: a
// `"` or `'` opens a run to its partner in which spaces keep the field whole.
// An unclosed quote runs to the end of the line rather than erroring — the
// parse renders live while the closing quote is not typed yet.
func splitAddFields(raw string) []addField {
	var out []addField
	var cur strings.Builder
	var quote rune   // the open quote character, 0 = none
	started := false // cur has consumed at least one rune (even into "")
	openedQuoted := false
	flush := func() {
		if started {
			out = append(out, addField{text: cur.String(), quoted: openedQuoted})
		}
		cur.Reset()
		started, openedQuoted = false, false
	}
	for _, r := range raw {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
			} else {
				cur.WriteRune(r)
			}
			started = true
		case r == '"' || r == '\'':
			if !started {
				openedQuoted = true
			}
			quote = r
			started = true
		case r == ' ' || r == '\t':
			flush()
		default:
			cur.WriteRune(r)
			started = true
		}
	}
	flush()
	return out
}

// parseAddLine splits the modal line into the title and its inline tokens.
// Title words keep their order and rejoin on single spaces — the one space
// form the splitter can give back.
func parseAddLine(raw string) (title string, tk addTokens) {
	var words []string
	for _, f := range splitAddFields(raw) {
		if f.quoted {
			words = append(words, f.text)
			continue
		}
		k, v, ok := strings.Cut(f.text, ":")
		if !ok {
			words = append(words, f.text)
			continue
		}
		switch strings.ToLower(k) {
		case "value", "effort":
			n, err := strconv.Atoi(v)
			if err != nil {
				tk.bad = append(tk.bad, f.text+" — not a number")
				continue
			}
			if strings.EqualFold(k, "value") {
				tk.value = n
			} else {
				tk.effort = n
			}
		case "due":
			if v == "" {
				tk.bad = append(tk.bad, f.text+" — needs a date")
				continue
			}
			tk.due = v // last one wins; the grammar itself stays board.ParseDue's
		case "dep":
			// Comma = several ids, the -q list form (and pflag's own CSV read
			// of --dep would split it there anyway — splitting here keeps the
			// chips honest about it).
			got := false
			for _, d := range strings.Split(v, ",") {
				if d != "" {
					tk.deps = append(tk.deps, d)
					got = true
				}
			}
			if !got {
				tk.bad = append(tk.bad, f.text+" — needs a task id")
			}
		case "check":
			if strings.TrimSpace(v) == "" {
				tk.bad = append(tk.bad, f.text+" — needs text")
				continue
			}
			tk.checks = append(tk.checks, v)
		case "ref":
			if v == "" {
				tk.bad = append(tk.bad, f.text+" — needs a file:line or URL")
				continue
			}
			tk.refs = append(tk.refs, v)
		case "label", "epic", "repo", "status", "lane":
			tk.bad = append(tk.bad, f.text+" — inherited from the filter; filter first, or quote it to keep it in the title")
		default:
			words = append(words, f.text)
		}
	}
	return strings.TrimSpace(strings.Join(words, " ")), tk
}

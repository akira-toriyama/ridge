package ui

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/akira-toriyama/ridge/internal/board"
)

// Quick add's inline tokens (t-69v9): the details `furrow add` takes as flags,
// typed in the modal's one line with the -q filter vocabulary where the two
// overlap (value:4 due:+1d). One line stays the modal's shape — a second stage
// per field would make six fields cost six prompts.
//
//	盤面から起票 value:4 effort:2 due:+1d dep:t-x check:"再現手順を書く" ref:ui/addmode.go is:draft
//
// Splitting is on spaces. A `"` or `'` is significant in exactly TWO spots —
// opening a field (the whole field is literal title text: the escape hatch
// for a title that itself contains `value:`) and immediately after a token
// key's colon (`check:"…"` carries prose) — and a LITERAL RUNE everywhere
// else, so `Don't stop` and `彼は"これ"と言った` reach the store verbatim
// (found by review: consuming quotes anywhere silently rewrote such titles
// AND swallowed every token behind the odd quote). Unknown keys (and
// anything after the first `:` of a known one) stay verbatim, so
// `ref:ui/addmode.go:42` and a bare URL both survive. The inherited keys
// (label:/epic:/repo:) and the focus-derived ones (status:/lane:) are
// refused with their own guidance rather than silently titled: a user typing
// them expects them to land.
//
// The parse itself never fails — the chips row renders it live on every
// keystroke, mid-word and all — so everything uncommittable lands in bad,
// with the reason the ⚠ row and the refusal note quote. Semantic checks that
// need no board (estimate range, the due grammar, the ref CSV caveat) run
// HERE so the ⚠ row shows them before Enter does; board.AddOptions.Validate
// stays the backstop on the store path.

// addTokens is one parse of the modal line.
type addTokens struct {
	value, effort int // 0 = absent (value:0 itself is refused into bad)
	due           string
	deps          []string
	checks        []string
	refs          []string
	draft         bool     // is:draft — furrow's `add --draft` (t-v4pp)
	bad           []string // "value:abc — not a number"
}

// apply copies the parsed details onto an inherited-context AddOptions. The
// two halves are disjoint fields except draft, which the filter can also
// inherit (is:draft) — either source makes the task a draft, so it ORs.
func (tk addTokens) apply(o board.AddOptions) board.AddOptions {
	o.Value, o.Effort, o.Due = tk.value, tk.effort, tk.due
	o.Deps, o.Checks, o.Refs = tk.deps, tk.checks, tk.refs
	if tk.draft {
		o.Draft = true
	}
	return o
}

// addField is one space-delimited field of the modal line. text has value
// quotes stripped; raw is the field as typed, which is what an error message
// must quote (`check:"" — needs text`, not `check: — needs text`). quoted
// marks a field that OPENED with a quote — literal title text, never a token.
type addField struct {
	text   string
	raw    string
	quoted bool
}

// tokenKeyColon matches a field buffer that has just spelled a token key and
// its colon — the one mid-field position where a quote opens a value run.
var tokenKeyColon = regexp.MustCompile(`(?i)^(value|effort|due|dep|check|ref):$`)

// splitAddFields splits the line on spaces with the two-position quote rule
// above. An unclosed quote runs to the end of the line rather than erroring —
// the parse renders live while the closing quote is not typed yet.
func splitAddFields(raw string) []addField {
	var out []addField
	var text, orig strings.Builder
	var quote rune // the open quote character, 0 = none
	started := false
	openedQuoted := false
	flush := func() {
		if started {
			out = append(out, addField{text: text.String(), raw: orig.String(), quoted: openedQuoted})
		}
		text.Reset()
		orig.Reset()
		started, openedQuoted = false, false
	}
	for _, r := range raw {
		switch {
		case quote != 0:
			orig.WriteRune(r)
			if r == quote {
				quote = 0
			} else {
				text.WriteRune(r)
			}
		case r == ' ' || r == '\t':
			flush()
		case r == '"' || r == '\'':
			orig.WriteRune(r)
			switch {
			case !started:
				openedQuoted, quote, started = true, r, true
			case tokenKeyColon.MatchString(text.String()):
				quote = r
			default:
				text.WriteRune(r) // a literal rune mid-word: Don't, 彼は"これ"
			}
		default:
			text.WriteRune(r)
			orig.WriteRune(r)
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
	word := func(f addField) {
		if f.text != "" {
			words = append(words, f.text)
		}
	}
	for _, f := range splitAddFields(raw) {
		if f.quoted {
			word(f)
			continue
		}
		k, v, ok := strings.Cut(f.text, ":")
		if !ok {
			word(f)
			continue
		}
		switch strings.ToLower(k) {
		case "value", "effort":
			n, err := strconv.Atoi(v)
			if err != nil {
				tk.bad = append(tk.bad, f.raw+" — not a number")
				continue
			}
			if n < 1 || n > 5 {
				// Deliberately stricter than furrow's silent clamp (measured:
				// --value 9 exits 0, stored as 5): a clamp would stamp an
				// estimate the user did not type.
				tk.bad = append(tk.bad, f.raw+" — want 1..5")
				continue
			}
			if strings.EqualFold(k, "value") {
				tk.value = n
			} else {
				tk.effort = n
			}
		case "due":
			if v == "" {
				tk.bad = append(tk.bad, f.raw+" — needs a date")
				continue
			}
			if _, err := board.ParseDue(v); err != nil {
				tk.bad = append(tk.bad, f.raw+" — not a date (YYYY-MM-DD / +1d …)")
				continue
			}
			tk.due = v // last one wins; the grammar's one spelling is ParseDue's
		case "dep":
			// Comma = several ids, the -q list form (and pflag's own CSV read
			// of --dep would split it there anyway — splitting here keeps the
			// chips honest about it).
			got := false
			for _, d := range strings.Split(v, ",") {
				if d == "" {
					continue
				}
				if strings.Contains(d, `"`) {
					// --dep is the same pflag CSV field as --ref; without
					// this mirror the refusal would be pflag's own parse
					// error after the round trip (re-review, finding A).
					tk.bad = append(tk.bad, f.raw+" — `\"` cannot ride furrow's CSV flag parsing")
					got = true
					continue
				}
				tk.deps = append(tk.deps, d)
				got = true
			}
			if !got {
				tk.bad = append(tk.bad, f.raw+" — needs a task id")
			}
		case "check":
			if strings.TrimSpace(v) == "" {
				tk.bad = append(tk.bad, f.raw+" — needs text")
				continue
			}
			tk.checks = append(tk.checks, v)
		case "ref":
			if v == "" {
				tk.bad = append(tk.bad, f.raw+" — needs a file:line or URL")
				continue
			}
			if strings.ContainsAny(v, `,"`) {
				// The t-pwrp caveat, surfaced live instead of on Enter.
				tk.bad = append(tk.bad, f.raw+" — `,` and `\"` cannot ride furrow's CSV flag parsing")
				continue
			}
			tk.refs = append(tk.refs, v)
		case "is":
			// The one is: value an ADD can mean: born without a repo
			// (furrow `add --draft`), same spelling as the -q filter term —
			// EqualFold like -q itself, which matches the value
			// case-insensitively (measured). Every other is: value describes
			// a state the store derives (blocked, overdue, …) — nothing an
			// add could stamp — so it is refused with its own guidance rather
			// than silently titled.
			if strings.EqualFold(v, "draft") {
				tk.draft = true
				continue
			}
			tk.bad = append(tk.bad, f.raw+" — only is:draft applies to an add; filter the view instead, or quote it to keep it in the title")
		case "label", "epic", "repo":
			tk.bad = append(tk.bad, f.raw+" — inherited from the filter; filter first, or quote it to keep it in the title")
		case "status", "lane":
			// NOT the filter's: the add lands in the focused column
			// (enterAdd), and inheritContext never reads these keys.
			tk.bad = append(tk.bad, f.raw+" — the add lands in the focused column; focus it first, or quote it to keep it in the title")
		default:
			word(f)
		}
	}
	return strings.TrimSpace(strings.Join(words, " ")), tk
}

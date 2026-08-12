// Package cli owns ridge's process boundary: flag parsing, provider
// selection, the headless dump/bench modes and the exit-code contract. It
// holds no domain logic — the board semantics live in internal/board, the
// rendering in internal/ui.
package cli

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/akira-toriyama/ridge/internal/board"
	"github.com/akira-toriyama/ridge/internal/store/furrowstore"
	"github.com/akira-toriyama/ridge/internal/store/memstore"
	"github.com/akira-toriyama/ridge/internal/ui"
)

// Code is ridge's exit-code contract — a public API for scripts and agents,
// so the meanings must stay stable.
type Code int

const (
	// CodeOK is a clean exit.
	CodeOK Code = 0
	// CodeRun is a runtime failure — the store was unreachable or the program
	// died. The invocation was well-formed; fix the environment and retry.
	CodeRun Code = 1
	// CodeUsage is a malformed invocation — an unknown -demo, an unopenable
	// -perflog, a flag combination with no coherent meaning. Fix the
	// arguments; retrying verbatim cannot succeed.
	CodeUsage Code = 2
)

// Execute runs ridge and returns the process exit code. It is the one funnel
// between every failure and the number the shell sees.
func Execute() Code { return run(os.Args[1:], os.Stdout, os.Stderr) }

// run is Execute with its process globals passed in, so the exit-code contract
// above can actually be asserted by a test.
func run(argv []string, stdout, stderr io.Writer) Code {
	// A private FlagSet with ContinueOnError, not the global one: ExitOnError
	// calls os.Exit from inside Parse, which makes every code path below
	// unreachable from a test and takes the exit code out of this funnel's
	// hands.
	fs := flag.NewFlagSet("ridge", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		dump = fs.Bool("dump", false, "render one frame to stdout at -cols x -rows and exit (no TTY needed; always the fixture)")
		// -cols/-rows, not -w/-h: `-h` is the one flag name every CLI reserves
		// for help, and binding it to a height made `ridge -h` fail with
		// "flag needs an argument: -h" and exit 2.
		cols      = fs.Int("cols", 240, "width for -dump (240 is the board's design floor)")
		rows      = fs.Int("rows", 40, "height for -dump")
		filter    = fs.String("filter", "", "initial filter query, e.g. 'lane:backlog is:blocked'")
		peek      = fs.Bool("peek", false, "-dump with the detail side-peek open")
		tree      = fs.Bool("tree", false, "-dump with the dep tree overlay open (implies -peek)")
		table     = fs.Bool("table", false, "-dump the table view")
		light     = fs.Bool("light", false, "light palette")
		plain     = fs.Bool("plain", false, "-dump without ANSI styling (diffable)")
		demo      = fs.String("demo", "", "-dump in a transient state: "+strings.Join(ui.DemoNames, "|")+" (requires -dump; always the fixture)")
		mock      = fs.Bool("mock", false, "serve the built-in fixture instead of the real furrow store")
		readonly  = fs.Bool("readonly", false, "serve the fixture as a schema-gated read-only board (implies -mock)")
		perflog   = fs.String("perflog", "", "append one 'op\\tms' line per furrow command to this file")
		benchload = fs.Bool("benchload", false, "load the real board once, print the latency breakdown, exit (read-only)")
		// Both spellings are DEFINED rather than left to flag's built-in
		// handler, which writes the usage block to stderr — explicitly asked-for
		// help is the payload, so `ridge --help | grep demo` has to work.
		help  = fs.Bool("help", false, "print this usage and exit")
		helpH = fs.Bool("h", false, "print this usage and exit")
	)

	if err := fs.Parse(argv); err != nil {
		// Parse already wrote the complaint and the usage block to stderr.
		return CodeUsage
	}
	if *help || *helpH {
		fs.SetOutput(stdout)
		fs.Usage()
		return CodeOK
	}
	if n := fs.NArg(); n > 0 {
		_, _ = fmt.Fprintf(stderr, "error: ridge takes no positional arguments, got %q\n", fs.Arg(0))
		return CodeUsage
	}

	// -demo is a -dump modifier (glossary), and it used to be READ only inside
	// the -dump branch while still forcing the fixture: a bare `-demo move`
	// launched an ordinary fixture TUI with the state silently dropped, and a
	// bare `-demo bogus` exited 0 without ever validating the name.
	if *demo != "" && !*dump {
		_, _ = fmt.Fprintf(stderr, "error: -demo %s needs -dump (it fixes one transient state into a single frame)\n", *demo)
		return CodeUsage
	}

	if *benchload {
		// Every other flag either contradicts benchload or is silently
		// dropped by it; saying so beats measuring something the caller did
		// not ask for. -mock in particular used to be accepted and ignored,
		// so `-benchload -mock` read the REAL store.
		for _, bad := range []struct {
			name string
			on   bool
		}{{"-mock", *mock}, {"-dump", *dump}, {"-readonly", *readonly}, {"-demo", *demo != ""}} {
			if bad.on {
				_, _ = fmt.Fprintf(stderr, "error: -benchload measures the real store; %s cannot apply\n", bad.name)
				return CodeUsage
			}
		}
		perf, err := perfHook(*perflog)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "error: -perflog:", err)
			return CodeUsage
		}
		return runBenchload(stdout, stderr, perf)
	}

	// -dump is the headless verification surface; it stays on the fixture so
	// its frames are deterministic and diffable. -demo is not listed: it is
	// refused above unless -dump is set, so it can no longer force the fixture
	// on its own.
	useMock := *mock || *dump || *readonly

	var (
		prov   board.Provider
		loadMS int
	)
	switch {
	case useMock && *readonly:
		// The one board state that cannot be produced by hand: reaching it for
		// real needs a store on an older schema. Without this the frame
		// carrying the read-only warning could only be unit-tested, never
		// looked at — which is how a regression that deleted that warning got
		// as far as review (t-04f8).
		prov = memstore.NewGated("board-behind")
	case useMock:
		prov = memstore.New()
	default:
		perf, err := perfHook(*perflog)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "error: -perflog:", err)
			return CodeUsage
		}
		start := time.Now()
		p, err := furrowstore.New(perf)
		if err != nil {
			_, _ = fmt.Fprintln(stderr, "error:", err)
			_, _ = fmt.Fprintln(stderr, "hint: `ridge -mock` runs the built-in fixture without a furrow store")
			return CodeRun
		}
		prov, loadMS = p, int(time.Since(start).Milliseconds())
	}

	m := ui.New(prov, ui.Options{
		Light:  *light,
		Filter: *filter,
		Table:  *table,
		Peek:   *peek,
		Tree:   *tree,
		LoadMS: loadMS,
	})

	if *dump {
		out, err := m.Dump(*cols, *rows, *demo, *plain)
		if err != nil {
			// The only failure Dump has is an unknown -demo name, which is a
			// malformed invocation.
			_, _ = fmt.Fprintln(stderr, "error:", err)
			return CodeUsage
		}
		_, _ = fmt.Fprintln(stdout, out)
		return CodeOK
	}

	if _, err := tea.NewProgram(m).Run(); err != nil {
		_, _ = fmt.Fprintln(stderr, "error:", err)
		return CodeRun
	}
	return CodeOK
}

// runBenchload answers t-s86r's standing question — what does opening the
// real board cost? — with reads only: the three furrow execs (concurrent),
// the body files, nothing written anywhere.
func runBenchload(stdout, stderr io.Writer, extra func(op string, d time.Duration)) Code {
	var mu sync.Mutex
	type sample struct {
		op string
		ms int64
	}
	var samples []sample
	perf := func(op string, d time.Duration) {
		mu.Lock()
		samples = append(samples, sample{op, d.Milliseconds()})
		mu.Unlock()
		// -perflog used to be dropped here: the branch returned before
		// perfHook ran at all, so the one mode whose purpose is measuring
		// furrow latency discarded the flag that persists the measurement.
		if extra != nil {
			extra(op, d)
		}
	}

	start := time.Now()
	p, err := furrowstore.New(perf)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "error:", err)
		return CodeRun
	}
	total := time.Since(start).Milliseconds()

	b := p.Board()
	bodies := 0
	for _, t := range b.Tasks() {
		if t.Body != "" {
			bodies++
		}
	}
	for _, s := range samples {
		_, _ = fmt.Fprintf(stdout, "%-8s %4dms (concurrent)\n", s.op, s.ms)
	}
	_, _ = fmt.Fprintf(stdout, "%-8s %4dms  %d tasks · %d epics · %d bodies read\n",
		"total", total, len(b.Tasks()), len(b.Epics()), bodies)
	return CodeOK
}

// perfHook returns the furrow-latency recorder: nil when unwanted, else an
// append-only "op\tms" line writer (the raw material for t-s86r's latency
// record). Failure to open the file is fatal — a measurement run that
// silently measures nothing is worse than no run.
func perfHook(path string) (func(op string, d time.Duration), error) {
	if path == "" {
		return nil, nil
	}
	// The path is the -perflog flag: the user chose where their own log goes (G304).
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec
	if err != nil {
		return nil, err
	}
	var mu sync.Mutex
	return func(op string, d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		// A failed perf line must not take the app down mid-gesture; the
		// worst case is a shorter log, which the analysis will notice.
		_, _ = fmt.Fprintf(f, "%s\t%d\n", op, d.Milliseconds())
	}, nil
}

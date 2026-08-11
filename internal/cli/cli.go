// Package cli owns ridge's process boundary: flag parsing, provider
// selection, the headless dump/bench modes and the exit-code contract. It
// holds no domain logic — the board semantics live in internal/board, the
// rendering in internal/ui.
package cli

import (
	"flag"
	"fmt"
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
	// -perflog. Fix the arguments; retrying verbatim cannot succeed.
	// flag.ExitOnError exits with 2 on unparseable flags, matching this.
	CodeUsage Code = 2
)

// Execute runs ridge and returns the process exit code. It is the one funnel
// between every failure and the number the shell sees.
func Execute() Code {
	var (
		dump      = flag.Bool("dump", false, "render one frame to stdout at -w x -h and exit (no TTY needed; always the fixture)")
		w         = flag.Int("w", 140, "width for -dump")
		h         = flag.Int("h", 40, "height for -dump")
		filter    = flag.String("filter", "", "initial filter query, e.g. 'lane:backlog is:blocked'")
		peek      = flag.Bool("peek", false, "-dump with the detail side-peek open")
		tree      = flag.Bool("tree", false, "-dump with the dep tree overlay open (implies -peek)")
		table     = flag.Bool("table", false, "-dump the table view")
		light     = flag.Bool("light", false, "light palette")
		plain     = flag.Bool("plain", false, "-dump without ANSI styling (diffable)")
		demo      = flag.String("demo", "", "-dump in a transient state: "+strings.Join(ui.DemoNames, "|")+" (always the fixture)")
		mock      = flag.Bool("mock", false, "serve the built-in fixture instead of the real furrow store")
		readonly  = flag.Bool("readonly", false, "serve the fixture as a schema-gated read-only board (implies -mock)")
		perflog   = flag.String("perflog", "", "append one 'op\\tms' line per furrow command to this file")
		benchload = flag.Bool("benchload", false, "load the real board once, print the latency breakdown, exit (read-only)")
	)
	flag.Parse()

	if *benchload {
		return runBenchload()
	}

	// -dump and -demo are the headless verification surface; they stay on the
	// fixture so their frames are deterministic and diffable.
	useMock := *mock || *dump || *demo != "" || *readonly

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
			fmt.Fprintln(os.Stderr, "error: -perflog:", err)
			return CodeUsage
		}
		start := time.Now()
		p, err := furrowstore.New(perf)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			fmt.Fprintln(os.Stderr, "hint: `ridge -mock` runs the built-in fixture without a furrow store")
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
		out, err := m.Dump(*w, *h, *demo, *plain)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			return CodeUsage
		}
		fmt.Println(out)
		return CodeOK
	}

	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return CodeRun
	}
	return CodeOK
}

// runBenchload answers t-s86r's standing question — what does opening the
// real board cost? — with reads only: the three furrow execs (concurrent),
// the body files, nothing written anywhere.
func runBenchload() Code {
	var mu sync.Mutex
	type sample struct {
		op string
		ms int64
	}
	var samples []sample
	perf := func(op string, d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		samples = append(samples, sample{op, d.Milliseconds()})
	}

	start := time.Now()
	p, err := furrowstore.New(perf)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
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
		fmt.Printf("%-8s %4dms (concurrent)\n", s.op, s.ms)
	}
	fmt.Printf("%-8s %4dms  %d tasks · %d epics · %d bodies read\n",
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

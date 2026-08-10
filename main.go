package main

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
)

func main() {
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
		demo      = flag.String("demo", "", "-dump in a transient state: move|drag|graph|help (always the fixture)")
		mock      = flag.Bool("mock", false, "serve the built-in fixture instead of the real furrow store")
		perflog   = flag.String("perflog", "", "append one 'op\\tms' line per furrow command to this file")
		benchload = flag.Bool("benchload", false, "load the real board once, print the latency breakdown, exit (read-only)")
	)
	flag.Parse()

	if *benchload {
		runBenchload()
		return
	}

	// -dump and -demo are the headless verification surface; they stay on the
	// fixture so their frames are deterministic and diffable.
	useMock := *mock || *dump || *demo != ""

	var (
		prov   Provider
		loadMS int
	)
	if useMock {
		prov = newMockProvider()
	} else {
		perf := perfHook(*perflog)
		start := time.Now()
		p, err := newFurrowProvider(perf)
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			fmt.Fprintln(os.Stderr, "hint: `ridge -mock` runs the built-in fixture without a furrow store")
			os.Exit(1)
		}
		prov, loadMS = p, int(time.Since(start).Milliseconds())
	}

	m := New(prov)
	if *light {
		m.th = newTheme(false)
	}
	if *filter != "" {
		m.ti.SetValue(*filter)
		m.applyFilter(*filter)
	}
	if *table {
		m.view = viewTable
	}
	if *peek || *tree {
		m.peekOpen = true
		m.treeOpen = *tree
	}
	if prov.Live() && m.b.Writable() {
		m.note("loaded %d tasks in %dms · r reload · R sync · ? help", len(m.b.Tasks()), loadMS)
	}

	if *dump {
		m.w, m.h = *w, *h
		m.help.SetWidth(*w)
		m.recompute()
		m.relayout()
		if err := m.demoState(*demo); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(2)
		}
		out := m.View().Content
		if *plain {
			out = ansiStrip(out)
		}
		fmt.Println(out)
		return
	}

	if _, err := tea.NewProgram(m).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// runBenchload answers t-s86r's standing question — what does opening the
// real board cost? — with reads only: the three furrow execs (concurrent),
// the body files, nothing written anywhere.
func runBenchload() {
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
	p, err := newFurrowProvider(perf)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
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
		"total", total, len(b.Tasks()), len(b.epics), bodies)
}

// perfHook returns the furrow-latency recorder: nil when unwanted, else an
// append-only "op\tms" line writer (the raw material for t-s86r's latency
// record). Failures to open the file are fatal — a measurement run that
// silently measures nothing is worse than no run.
func perfHook(path string) func(op string, d time.Duration) {
	if path == "" {
		return nil
	}
	// The path is the -perflog flag: the user chose where their own log goes (G304).
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644) //nolint:gosec
	if err != nil {
		fmt.Fprintln(os.Stderr, "error: -perflog:", err)
		os.Exit(2)
	}
	var mu sync.Mutex
	return func(op string, d time.Duration) {
		mu.Lock()
		defer mu.Unlock()
		// A failed perf line must not take the app down mid-gesture; the
		// worst case is a shorter log, which the analysis will notice.
		_, _ = fmt.Fprintf(f, "%s\t%d\n", op, d.Milliseconds())
	}
}

// demoState puts the model into a transient state that a single -dump frame
// could not otherwise reach, because it only exists mid-gesture. Without this,
// "does the drop indicator render?" is a question only a human at a terminal
// can answer — and the house rule is that everything is provable headless.
func (m *Model) demoState(kind string) error {
	switch kind {
	case "":
		return nil

	case "help":
		m.fullHelp = true

	case "move":
		m.curLane = m.b.LaneIndex("backlog")
		m.setPos(1)
		m.enterMove()
		m.dropLane, m.dropIdx = "ready", 1
		m.followDrop()

	case "drag":
		src := m.lay.Col("backlog")
		dst := m.lay.Col("ready")
		if src == nil || dst == nil || len(src.Cards) < 2 {
			return fmt.Errorf("demo drag: the board is too small at this size")
		}
		grab := src.Cards[1]
		m.Update(tea.MouseClickMsg{X: grab.X + 3, Y: grab.Y + 1, Button: tea.MouseLeft})
		m.Update(tea.MouseMotionMsg{X: dst.X + 8, Y: dst.Top + 4, Button: tea.MouseLeft})

	case "graph":
		// Root the graph on a task that actually HAS both directions, so the
		// frame proves the layout rather than a degenerate single node.
		m.curLane = m.b.LaneIndex("backlog")
		for i, t := range m.cols["backlog"] {
			if len(t.Deps) > 0 && len(m.g.Blocks(t.ID)) > 0 {
				m.setPos(i)
				break
			}
		}
		m.openGraph()

	default:
		return fmt.Errorf("unknown -demo %q (want move|drag|graph|help)", kind)
	}
	m.relayout()
	return nil
}

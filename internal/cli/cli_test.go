package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	lg "charm.land/lipgloss/v2"

	"github.com/akira-toriyama/ridge/internal/ui"
)

// The live path reads the user's real views.toml (views.DefaultPath), so the
// XDG root is pinned to an empty temp dir for the whole package — a test must
// never vary with this machine's config, even one that only reaches the live
// path by a future edit.
func TestMain(m *testing.M) {
	// Helper-process mode for TestDumpFramesDoNotDependOnTheProcessTZ: the
	// process zone is read once at startup, so varying it takes a subprocess.
	// It returns before the XDG pin below, so the helper must inherit the
	// parent's environment (the test appends to os.Environ) or -dump would
	// read the developer's real views.toml; and the variable left in a shell
	// turns every run of this package into a frame dump.
	if args := os.Getenv("RIDGE_TEST_DUMP_ARGS"); args != "" {
		os.Exit(int(run(strings.Fields(args), os.Stdout, os.Stderr)))
	}
	dir, err := os.MkdirTemp("", "ridge-cli-xdg-*")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("XDG_CONFIG_HOME", dir)
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// The exit-code contract is declared "a public API for scripts and agents, so
// the meanings must stay stable" — and nothing asserted a single return site.
// These drive run() with its writers injected, so no test touches os.Args or
// the global FlagSet.

func runArgs(t *testing.T, args ...string) (Code, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := run(args, &out, &errb)
	return code, out.String(), errb.String()
}

// setViewsToml points XDG at a fresh dir holding the given views.toml and
// returns that dir. t.Setenv overrides TestMain's package-wide pin for one
// test.
func setViewsToml(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	if err := os.MkdirAll(filepath.Join(dir, "ridge"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ridge", "views.toml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return dir
}

// A views.toml that cannot be parsed refuses the LIVE invocation before any
// file is created or any store is contacted — semantic typos are clamped
// inside views.Load instead and never reach this refusal.
func TestMalformedViewsTomlRefusesTheLiveInvocation(t *testing.T) {
	setViewsToml(t, "[[view]\nbroken")
	code, _, errb := runArgs(t)
	if code != CodeRun {
		t.Fatalf("exit %d, want %d (CodeRun): %s", code, CodeRun, errb)
	}
	if !strings.Contains(errb, "views.toml") {
		t.Errorf("stderr %q does not name the file to fix", errb)
	}
}

// The fixture paths must not vary with this machine's views.toml — a -dump
// frame is deterministic chrome, and -demo views injects its own set. This
// is the wiring pin the -debuglog test exists for, on the views port.
func TestDumpIgnoresTheMachinesViewsToml(t *testing.T) {
	setViewsToml(t, "[[view]]\nname = \"SHOULD-NOT-APPEAR\"\n")
	code, out, errb := runArgs(t, "-dump", "-plain")
	if code != CodeOK {
		t.Fatalf("-dump exited %d: %s", code, errb)
	}
	if strings.Contains(out, "SHOULD-NOT-APPEAR") {
		t.Error("-dump rendered the machine's views.toml into a fixture frame")
	}
}

// `ridge -h` is the invocation everyone types first. Binding -h to the dump
// height made it fail with "flag needs an argument: -h" and exit 2.
func TestHelpFlagsPrintUsageToStdoutAndExitOK(t *testing.T) {
	for _, arg := range []string{"-h", "-help", "--help"} {
		t.Run(arg, func(t *testing.T) {
			code, out, errb := runArgs(t, arg)
			if code != CodeOK {
				t.Errorf("%s exited %d, want %d", arg, code, CodeOK)
			}
			// Explicitly asked-for help IS the payload: `ridge --help | grep
			// demo` has to work, so it goes to stdout, not stderr.
			if !strings.Contains(out, "-demo") {
				t.Errorf("%s did not print the usage block to stdout; stdout=%q stderr=%q", arg, out, errb)
			}
			if errb != "" {
				t.Errorf("%s wrote to stderr: %q", arg, errb)
			}
		})
	}
}

func TestUnknownFlagIsAUsageError(t *testing.T) {
	code, out, errb := runArgs(t, "-nope")
	if code != CodeUsage {
		t.Errorf("an unknown flag exited %d, want %d", code, CodeUsage)
	}
	if errb == "" {
		t.Error("an unknown flag said nothing on stderr")
	}
	if out != "" {
		t.Errorf("an unknown flag wrote to stdout: %q", out)
	}
}

func TestPositionalArgumentsAreRefused(t *testing.T) {
	code, _, errb := runArgs(t, "somefile.md")
	if code != CodeUsage {
		t.Errorf("a positional argument exited %d, want %d", code, CodeUsage)
	}
	if !strings.Contains(errb, "positional") {
		t.Errorf("the refusal did not say why: %q", errb)
	}
}

// -demo is a -dump modifier. Without -dump it used to force the fixture, drop
// the requested state and launch an ordinary TUI — and never validate the name.
func TestDemoWithoutDumpIsRefusedRatherThanSilentlyDropped(t *testing.T) {
	for _, name := range []string{"move", "bogus"} {
		code, _, errb := runArgs(t, "-demo", name)
		if code != CodeUsage {
			t.Errorf("-demo %s exited %d, want %d", name, code, CodeUsage)
		}
		if !strings.Contains(errb, "-dump") {
			t.Errorf("-demo %s did not explain that -dump is required: %q", name, errb)
		}
	}
}

func TestUnknownDemoNameIsAUsageError(t *testing.T) {
	code, _, errb := runArgs(t, "-dump", "-demo", "definitely-not-a-state")
	if code != CodeUsage {
		t.Errorf("an unknown -demo exited %d, want %d", code, CodeUsage)
	}
	if !strings.Contains(errb, "definitely-not-a-state") {
		t.Errorf("the refusal did not name the bad state: %q", errb)
	}
}

// Every advertised -demo must actually render through the real flag path.
func TestEveryDemoNameDumpsThroughTheFlagPath(t *testing.T) {
	for _, name := range ui.DemoNames {
		code, out, errb := runArgs(t, "-dump", "-plain", "-cols", "240", "-rows", "40", "-demo", name)
		if code != CodeOK {
			t.Errorf("-demo %s exited %d: %s", name, code, errb)
			continue
		}
		if strings.TrimSpace(out) == "" {
			t.Errorf("-demo %s rendered an empty frame", name)
		}
	}
}

// -benchload measures the REAL store. -mock was accepted and ignored, so
// `-benchload -mock` read the real board while the caller asked for the fixture.
func TestBenchloadRefusesFlagsItCannotHonour(t *testing.T) {
	for _, arg := range []string{"-mock", "-dump", "-readonly"} {
		code, _, errb := runArgs(t, "-benchload", arg)
		if code != CodeUsage {
			t.Errorf("-benchload %s exited %d, want %d", arg, code, CodeUsage)
		}
		if !strings.Contains(errb, arg) {
			t.Errorf("-benchload %s did not name the offending flag: %q", arg, errb)
		}
	}
}

// fullScreenViews is every opening-view flag whose view has no side panel,
// with the badge its title bar carries. -table is the one opening view that
// keeps the board's chrome (its own tests below say so where it matters).
var fullScreenViews = map[string]string{
	"-roadmap": "⟨ROADMAP⟩", "-graph": "⟨GRAPH⟩", "-map": "⟨MAP⟩",
	"-boxes": "⟨BOXES⟩", "-swim": "⟨SWIM⟩", "-sweep": "⟨SWEEP⟩",
}

func openingViews() []string {
	views := []string{"-table"}
	for v := range fullScreenViews {
		views = append(views, v)
	}
	sort.Strings(views)
	return views
}

// Each opening-view flag is a view setting like -table: it must reach a
// -dump frame, carrying that view's own badge.
func TestEveryOpeningViewFlagDumpsHeadless(t *testing.T) {
	for view, badge := range fullScreenViews {
		code, out, errb := runArgs(t, "-dump", view, "-plain")
		if code != CodeOK {
			t.Fatalf("-dump %s exited %d, want %d; stderr=%q", view, code, CodeOK, errb)
		}
		if !strings.Contains(out, badge) {
			t.Errorf("-dump %s: the frame does not carry %s:\n%s", view, badge, out)
		}
	}
}

// Two flags that each name the opening view have no coherent composition —
// last-flag-wins would make one of them a silent no-op. Every pair.
func TestTwoOpeningViewsAreRefused(t *testing.T) {
	views := openingViews()
	for i, a := range views {
		for _, b := range views[i+1:] {
			code, _, errb := runArgs(t, a, b)
			if code != CodeUsage {
				t.Errorf("%s %s exited %d, want %d", a, b, code, CodeUsage)
			}
			if !strings.Contains(errb, a) || !strings.Contains(errb, b) {
				t.Errorf("%s %s: the refusal did not name both flags: %q", a, b, errb)
			}
		}
	}
}

// The peek is board/table chrome no full-screen view composites: honouring
// the pair would ship a silent no-op (-table -peek, by contrast, works).
func TestFullScreenViewsRefusePeekAndTree(t *testing.T) {
	for view := range fullScreenViews {
		for _, arg := range []string{"-peek", "-tree"} {
			code, _, errb := runArgs(t, view, arg)
			if code != CodeUsage {
				t.Errorf("%s %s exited %d, want %d", view, arg, code, CodeUsage)
			}
			if !strings.Contains(errb, view) {
				t.Errorf("%s %s refusal did not name the view: %q", view, arg, errb)
			}
		}
	}
	if code, _, errb := runArgs(t, "-dump", "-table", "-peek"); code != CodeOK {
		t.Errorf("-dump -table -peek exited %d, want %d (the table composites the peek): %q", code, CodeOK, errb)
	}
}

// The read-only warning is set once per session and never restored, so no
// view flag may write status over it — the exact regression the repo
// records shipping once, and the first cut of -roadmap shipped it again
// (caught in review): openRoadmap's note landed where the load note
// deliberately says nothing. Every opening view, so the next flag cannot
// ship it a third time.
func TestEveryOpeningViewFlagKeepsTheReadOnlyWarning(t *testing.T) {
	for _, view := range openingViews() {
		code, out, errb := runArgs(t, "-dump", "-readonly", view, "-plain")
		if code != CodeOK {
			t.Fatalf("-dump -readonly %s exited %d, want %d; stderr=%q", view, code, CodeOK, errb)
		}
		if !strings.Contains(out, "read-only") {
			t.Errorf("-dump -readonly %s: the read-only warning is gone from the frame:\n%s", view, out)
		}
	}
}

// -live is a -dump modifier like -demo, and refused without it for the same
// reason: the interactive session already reads the real store, so a bare
// -live would be a silent no-op.
func TestLiveNeedsDump(t *testing.T) {
	code, _, errb := runArgs(t, "-live")
	if code != CodeUsage {
		t.Errorf("-live exited %d, want %d", code, CodeUsage)
	}
	if !strings.Contains(errb, "-dump") {
		t.Errorf("-live did not explain that -dump is required: %q", errb)
	}
}

// -live names the real store; -mock/-readonly name the fixture; -demo is the
// fixture's harness (one of its states archives through the provider). None
// of the three may be silently outvoted — and no store is read for a refused
// invocation, which the FURROW_DIR pin below proves the cheap way: a read
// would fail on it, and the exit is still usage, not run.
func TestLiveRefusesTheFixtureFlags(t *testing.T) {
	t.Setenv("FURROW_DIR", filepath.Join(t.TempDir(), "no-store-here"))
	for _, extra := range [][]string{{"-mock"}, {"-readonly"}, {"-demo", "sweeprestore"}} {
		args := append([]string{"-dump", "-live"}, extra...)
		code, _, errb := runArgs(t, args...)
		if code != CodeUsage {
			t.Errorf("%v exited %d, want %d", args, code, CodeUsage)
		}
		if !strings.Contains(errb, extra[0]) {
			t.Errorf("%v: the refusal did not name %s: %q", args, extra[0], errb)
		}
	}
}

// labStore seeds a throwaway furrow store under FURROW_DIR — which outranks
// the working directory, so the dump under test reads THIS board and never
// the developer's — and returns the one task's title. Skipped without a
// furrow binary (the contract tests' rule).
func labStore(t *testing.T) string {
	t.Helper()
	if testing.Short() {
		t.Skip("execs furrow; skipped in -short")
	}
	if _, err := exec.LookPath("furrow"); err != nil {
		t.Skip("furrow binary not on PATH")
	}
	dir := t.TempDir()
	t.Setenv("FURROW_DIR", filepath.Join(dir, ".furrow"))
	t.Setenv("FURROW_BOARD", "")
	// Short enough to sit on one card line at -cols 240 (a longer Japanese
	// title wraps inside the card, and a substring match then misses it).
	const title = "実盤の題名"
	for _, args := range [][]string{
		{"git", "init", "-q"},
		{"furrow", "init"},
		{"furrow", "add", title, "-r", "lab/lab"},
	} {
		cmd := exec.Command(args[0], args[1:]...) //nolint:gosec // seeding the throwaway store IS the test
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	return title
}

// -dump -live draws the store furrow resolves (FURROW_DIR here), at rest:
// the real title, the live load note, and — like every other -dump — none of
// this machine's views.toml. -perflog is honoured on this path, because the
// three reads are furrow execs.
func TestLiveDumpDrawsTheStoreFurrowResolves(t *testing.T) {
	title := labStore(t)
	setViewsToml(t, "[[view]]\nname = \"機械の癖\"\nlayout = \"table\"\n")
	log := filepath.Join(t.TempDir(), "perf.tsv")

	code, out, errb := runArgs(t, "-dump", "-live", "-plain", "-perflog", log)
	if code != CodeOK {
		t.Fatalf("-dump -live exited %d, want %d; stderr=%q", code, CodeOK, errb)
	}
	if !strings.Contains(out, title) {
		t.Errorf("the live frame does not carry the store's task:\n%s", out)
	}
	if !strings.Contains(out, "loaded 1 tasks in") {
		t.Errorf("the live frame does not carry the live load note:\n%s", out)
	}
	if strings.Contains(out, "機械の癖") {
		t.Error("-dump -live rendered the machine's views.toml into the frame")
	}
	b, err := os.ReadFile(log) //nolint:gosec // G304: the test's own temp file
	if err != nil || !strings.Contains(string(b), "\t") {
		t.Errorf("-perflog on a live dump wrote nothing: err=%v content=%q", err, b)
	}
	// Every full-screen view reaches a live frame too — the reason the
	// opening-view flags exist (the graph roots on the one task).
	for view, badge := range fullScreenViews {
		code, out, errb := runArgs(t, "-dump", "-live", "-plain", view)
		if code != CodeOK {
			t.Fatalf("-dump -live %s exited %d, want %d; stderr=%q", view, code, CodeOK, errb)
		}
		if !strings.Contains(out, badge) {
			t.Errorf("-dump -live %s: the frame does not carry %s:\n%s", view, badge, out)
		}
	}
}

// An unopenable -perflog is fatal: a measurement run that silently measures
// nothing is worse than no run.
func TestUnopenablePerflogIsAUsageError(t *testing.T) {
	// A path whose parent is a FILE, so the open cannot succeed.
	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errb := runArgs(t, "-benchload", "-perflog", filepath.Join(f, "log.tsv"))
	if code != CodeUsage {
		t.Errorf("an unopenable -perflog exited %d, want %d", code, CodeUsage)
	}
	if !strings.Contains(errb, "perflog") {
		t.Errorf("the refusal did not name -perflog: %q", errb)
	}
}

// -debuglog records the interactive event loop; the two loop-less modes must
// refuse it rather than accept a flag that would record nothing.
func TestDebuglogIsRefusedWhereNoSessionExists(t *testing.T) {
	log := filepath.Join(t.TempDir(), "debug.jsonl")
	for _, argv := range [][]string{
		{"-dump", "-debuglog", log},
		{"-benchload", "-debuglog", log},
	} {
		code, _, errb := runArgs(t, argv...)
		if code != CodeUsage {
			t.Errorf("%v exited %d, want %d", argv, code, CodeUsage)
		}
		if !strings.Contains(errb, "debuglog") {
			t.Errorf("%v did not name -debuglog: %q", argv, errb)
		}
	}
	if _, err := os.Stat(log); err == nil {
		t.Error("a refused -debuglog still created the file")
	}
}

// The one test that pins the cli→ui wiring. A test process has no TTY, so
// the program itself cannot start (CodeRun) — but by then the recorder was
// built (session/start is NewDebugLog's own first line) AND handed through
// Options.Debug (session/board is emitted by ui.New). Review found that
// without this, `Debug: nil` — a -debuglog that records nothing — passed the
// entire suite.
func TestDebuglogWiringSurvivesToTheModel(t *testing.T) {
	log := filepath.Join(t.TempDir(), "debug.jsonl")
	code, _, errb := runArgs(t, "-mock", "-debuglog", log)
	if code != CodeRun {
		t.Fatalf("-mock -debuglog in a no-TTY test exited %d, want %d: %s", code, CodeRun, errb)
	}
	b, err := os.ReadFile(log) //nolint:gosec // the t.TempDir path built above
	if err != nil {
		t.Fatalf("no log written: %v", err)
	}
	for _, want := range []string{`"kind":"start"`, `"kind":"board"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("log lacks %s:\n%s", want, b)
		}
	}
	// The file carries every keystroke verbatim, so it must be private to
	// its owner — unlike -perflog's op/ms pairs.
	if fi, err := os.Stat(log); err == nil && fi.Mode().Perm() != 0o600 {
		t.Errorf("-debuglog created %v, want -rw-------", fi.Mode().Perm())
	}
}

// A refusal that happens AFTER another flag already failed must not leave a
// created -debuglog behind: -perflog opens first precisely so its failure
// precedes the first file the invocation would create.
func TestFailedPerflogCreatesNoDebuglog(t *testing.T) {
	bad := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(bad, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(t.TempDir(), "debug.jsonl")
	code, _, _ := runArgs(t, "-perflog", filepath.Join(bad, "p.tsv"), "-debuglog", log)
	if code != CodeUsage {
		t.Fatalf("unopenable -perflog exited %d, want %d", code, CodeUsage)
	}
	if _, err := os.Stat(log); err == nil {
		t.Error("the refused invocation still created the -debuglog file")
	}
}

// An unopenable -debuglog is fatal before the TUI ever starts, same contract
// as -perflog: a debug run that silently records nothing is worse than none.
// -mock keeps the check off the real store; run() returns before NewProgram.
func TestUnopenableDebuglogIsAUsageError(t *testing.T) {
	f := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	code, _, errb := runArgs(t, "-mock", "-debuglog", filepath.Join(f, "log.jsonl"))
	if code != CodeUsage {
		t.Errorf("an unopenable -debuglog exited %d, want %d", code, CodeUsage)
	}
	if !strings.Contains(errb, "debuglog") {
		t.Errorf("the refusal did not name -debuglog: %q", errb)
	}
}

// -readonly is wired to the gated provider ONLY here, so deleting that arm
// survives every other test in the repo.
func TestReadonlyServesTheSchemaGatedBoard(t *testing.T) {
	code, out, errb := runArgs(t, "-dump", "-plain", "-cols", "240", "-rows", "40", "-readonly")
	if code != CodeOK {
		t.Fatalf("-readonly -dump exited %d: %s", code, errb)
	}
	if !strings.Contains(out, "read-only") {
		t.Error("the -readonly frame does not carry the read-only warning — the one state that cannot be produced by hand")
	}
}

// The plain dump is the diffable one, so it must carry no escape sequences.
func TestPlainDumpHasNoAnsi(t *testing.T) {
	code, out, errb := runArgs(t, "-dump", "-plain", "-cols", "240", "-rows", "40")
	if code != CodeOK {
		t.Fatalf("exited %d: %s", code, errb)
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("-plain emitted ANSI, so dumps are not diffable")
	}
}

// -dump defaults to the board's own design floor rather than a width the
// project declares unsupported.
func TestDumpDefaultsToTheDesignFloor(t *testing.T) {
	code, out, errb := runArgs(t, "-dump", "-plain")
	if code != CodeOK {
		t.Fatalf("exited %d: %s", code, errb)
	}
	// DISPLAY width, not rune count: the fixture is Japanese and one rune is
	// two cells, so counting runes understates every line that carries a title.
	widest := 0
	for _, line := range strings.Split(out, "\n") {
		if n := lg.Width(line); n > widest {
			widest = n
		}
	}
	if widest != 240 {
		t.Errorf("the default dump is %d cells wide; the README declares 240 the floor and -cols defaults to it", widest)
	}
}

// -benchload used to accept and ignore most of the flag surface. The refusal
// is driven off what the caller TYPED, so a flag left at its default never
// trips it.
//
// The flags to try are read back off `ridge -h` rather than listed here: a
// hand-written copy passes for every flag it happens to name, which makes it
// silent on precisely the case it exists for — a flag added to run() and
// forgotten in cli.go's list. It caught two on the day it was written.
func TestBenchloadRefusesEveryFrameShapingFlag(t *testing.T) {
	// What -benchload legitimately composes with, plus both spellings of help.
	// Everything else shapes a frame or swaps the store.
	allowed := map[string]bool{"benchload": true, "perflog": true, "help": true, "h": true}
	surface := flagSurface(t)
	names := make([]string, 0, len(surface))
	for name := range surface {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if allowed[name] {
			continue
		}
		args := []string{"-benchload", "-" + name}
		if !surface[name] {
			args = append(args, "1") // a value flag needs one to parse at all
		}
		code, _, errb := runArgs(t, args...)
		if code != CodeUsage {
			t.Errorf("%v exited %d, want %d — it shapes a frame and is being ignored",
				args, code, CodeUsage)
		}
		if !strings.Contains(errb, name) {
			t.Errorf("%v did not name the offending flag: %q", args, errb)
		}
	}
}

// flagSurface reads the flag set off `ridge -h`, which is the block
// flag.PrintDefaults writes: "  -name" for a bool, "  -name type" otherwise.
func flagSurface(t *testing.T) map[string]bool {
	t.Helper()
	code, out, _ := runArgs(t, "-h")
	if code != CodeOK {
		t.Fatalf("-h exited %d", code)
	}
	isBool := map[string]bool{}
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "  -") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, "  -"))
		if len(fields) == 0 {
			continue
		}
		isBool[fields[0]] = len(fields) == 1
	}
	if len(isBool) < 10 {
		t.Fatalf("parsed %d flags out of -h; the usage format changed", len(isBool))
	}
	return isBool
}

// -perflog measures furrow execs. On the fixture there are none, and perfHook
// is not even consulted — so accepting it promised a log that never gets
// written. A -dump without -live is the fixture too.
func TestPerflogIsRefusedOnTheFixture(t *testing.T) {
	log := filepath.Join(t.TempDir(), "perf.tsv")
	for _, arg := range []string{"-mock", "-dump", "-readonly"} {
		code, _, errb := runArgs(t, arg, "-perflog", log)
		if code != CodeUsage {
			t.Errorf("%s -perflog exited %d, want %d", arg, code, CodeUsage)
		}
		if !strings.Contains(errb, "perflog") {
			t.Errorf("%s -perflog did not explain itself: %q", arg, errb)
		}
	}
	if _, err := os.Stat(log); err == nil {
		t.Error("a refused -perflog still created the file")
	}
}

// A flag left at its default must not trip the benchload refusal.
func TestBenchloadAcceptsAnUntypedDefault(t *testing.T) {
	// -cols has a non-zero default; testing values rather than what was typed
	// would refuse every benchload run.
	code, _, errb := runArgs(t, "-benchload", "-perflog", filepath.Join(t.TempDir(), "p.tsv"))
	if code == CodeUsage && strings.Contains(errb, "cannot apply") {
		t.Errorf("-benchload refused itself over an untyped default: %q", errb)
	}
}

// The fixture's frames are a function of its declared calendar (memstore
// declares JST), not the process zone: -dump renders the same -table and
// -roadmap frame with TZ=UTC and TZ=Pacific/Auckland in the environment.
// Auckland is where the fixture's 14:59:59Z dues fall on the next day — the
// shift t-kt2h measured on the real board. Re-runs this test binary in
// TestMain's helper mode, because time.Local is fixed at process start.
func TestDumpFramesDoNotDependOnTheProcessTZ(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	frame := func(tz, view string) string {
		t.Helper()
		cmd := exec.Command(exe, "-test.run=^$") //nolint:gosec // re-running this test binary IS the test
		cmd.Env = append(os.Environ(), "TZ="+tz, "RIDGE_TEST_DUMP_ARGS=-dump -plain "+view+" -cols 240 -rows 50")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("TZ=%s %s: %v", tz, view, err)
		}
		return string(out)
	}
	for _, view := range []string{"-table", "-roadmap"} {
		if utc, nz := frame("UTC", view), frame("Pacific/Auckland", view); utc != nz {
			t.Errorf("%s frame changed with TZ — the fixture store must declare its calendar:\n--- UTC\n%s\n--- Pacific/Auckland\n%s", view, utc, nz)
		}
	}
}

package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/akira-toriyama/ridge/internal/ui"
)

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
	widest := 0
	for _, line := range strings.Split(out, "\n") {
		if n := len([]rune(line)); n > widest {
			widest = n
		}
	}
	if widest < 200 {
		t.Errorf("the default dump is only %d cells wide; the README declares 240 the floor", widest)
	}
}

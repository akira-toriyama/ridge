package furrowstore

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// furrowError is furrow's machine-readable error envelope, decoded from
// stderr. No production caller branches on it: Error() folds Kind and Subject
// — and a sync-conflict's paths, ahead of the prose, since the status line
// truncates its right end — into the one line the ui shows. Retryable is kept because
// TestContractErrorsCarryTheEnvelope holds furrow to its promise that an
// unknown id is not retryable; a caller that ever needs to branch branches on
// Kind (a closed kebab-case vocabulary — `furrow vocab error-kinds`), never on
// the message prose.
type furrowError struct {
	Kind      string `json:"kind"`
	Subject   string `json:"subject"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	// Details.Paths: the conflicted paths beside a sync-conflict's message
	// (furrow internal/app/sync.go; t-36k0). Read for that kind alone —
	// sync-unmerged carries them too, and its message already lists them.
	Details struct {
		Paths []string `json:"paths"`
	} `json:"details"`
}

func (e *furrowError) Error() string {
	var s string
	if e.Subject != "" {
		s = fmt.Sprintf("%s (%s: %s)", e.Message, e.Kind, e.Subject)
	} else {
		s = fmt.Sprintf("%s (%s)", e.Message, e.Kind)
	}
	if n := len(e.Details.Paths); n > 0 && e.Kind == "sync-conflict" {
		// The paths LEAD: `synced: ` plus furrow's 181-cell message plus
		// the kind already fills 200 of the status row's 240 cells, so a
		// clause at the end was cut at the floor (measured 2026-09-28: one
		// path lost its `.json`, a second was invisible). Three are named,
		// the rest counted — with three of furrow's 27-cell shard paths the
		// clause is under 100 cells and the message's head still reads.
		const shown = 3
		paths := e.Details.Paths
		more := ""
		if n > shown {
			paths, more = paths[:shown], fmt.Sprintf(" +%d more", n-shown)
		}
		s = "conflicted paths: " + strings.Join(paths, ", ") + more + " — " + s
	}
	return s
}

// errorEnvelope is the stderr wrapper around furrowError.
type errorEnvelope struct {
	Error *furrowError `json:"error"`
}

// furrowClient execs the furrow binary and speaks its CLI/JSON contract:
// pure data on stdout, an {"error":{...}} envelope on stderr, exit 0 for ok
// (an empty query result included).
type furrowClient struct {
	bin     string
	dir     string // working directory; "" inherits the process cwd, which is how the board is resolved
	timeout time.Duration
	perf    func(op string, d time.Duration) // optional latency hook; also fed by failures
}

func newFurrowClient() *furrowClient {
	return &furrowClient{bin: "furrow", timeout: 15 * time.Second}
}

// run execs one furrow command and returns its stdout. op labels the call for
// the perf hook only — args carry the real command.
func (c *furrowClient) run(op string, args ...string) ([]byte, error) {
	return c.execute(op, c.timeout, nil, args...)
}

// runTimeout is run with an explicit deadline, for the commands that hit the
// network (sync) rather than the local store.
func (c *furrowClient) runTimeout(op string, timeout time.Duration, args ...string) ([]byte, error) {
	return c.execute(op, timeout, nil, args...)
}

// runStdin is run with stdin bytes attached, for the flags that read furrow's
// `-`=stdin convention (`edit --body -`). Every other run keeps a nil stdin —
// os/exec then hands the child /dev/null, which PersistNote's refusal of a
// bare `-` note depends on.
func (c *furrowClient) runStdin(op string, stdin []byte, args ...string) ([]byte, error) {
	return c.execute(op, c.timeout, stdin, args...)
}

func (c *furrowClient) execute(op string, timeout time.Duration, stdin []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, c.bin, args...) //nolint:gosec // G204: executing furrow with composed args IS the feature — ridge is a CLI/JSON client by contract
	cmd.Dir = c.dir
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	start := time.Now()
	err := cmd.Run()
	if c.perf != nil {
		c.perf(op, time.Since(start))
	}
	if err == nil {
		return stdout.Bytes(), nil
	}
	if ctx.Err() == context.DeadlineExceeded {
		return nil, fmt.Errorf("furrow %s timed out after %s", op, timeout)
	}

	// A non-zero exit writes the error envelope to stderr; stdout stays pure
	// data. Fall back to the raw stderr for anything that is not furrow's own
	// refusal (a missing binary errors before this point, via cmd.Run).
	var env errorEnvelope
	if jsonErr := json.Unmarshal(stderr.Bytes(), &env); jsonErr == nil && env.Error != nil {
		return nil, env.Error
	}
	msg := strings.TrimSpace(stderr.String())
	if msg == "" {
		msg = err.Error()
	}
	return nil, fmt.Errorf("furrow %s: %s", op, msg)
}

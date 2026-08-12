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
// stderr. Callers branch on Kind (a closed kebab-case vocabulary — `furrow
// vocab error-kinds`) and Retryable, never on the message prose.
type furrowError struct {
	Kind      string `json:"kind"`
	Subject   string `json:"subject"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
	Exit      int    `json:"exit"`
}

func (e *furrowError) Error() string {
	if e.Subject != "" {
		return fmt.Sprintf("%s (%s: %s)", e.Message, e.Kind, e.Subject)
	}
	return fmt.Sprintf("%s (%s)", e.Message, e.Kind)
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
	return c.runTimeout(op, c.timeout, args...)
}

// runTimeout is run with an explicit deadline, for the commands that hit the
// network (sync) rather than the local store.
func (c *furrowClient) runTimeout(op string, timeout time.Duration, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	// Executing the furrow binary with composed args IS the feature: ridge is
	// a CLI/JSON client by contract (G204).
	cmd := exec.CommandContext(ctx, c.bin, args...) //nolint:gosec
	cmd.Dir = c.dir
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

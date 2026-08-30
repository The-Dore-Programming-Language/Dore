// Package crucible runs untrusted code for the assay.
//
// The cupel is a lint; this is the boundary. Every assay run executes code the
// compiler did not write — eventually code a model wrote — so it runs in a
// separate process with a scrubbed environment and a hard deadline.
//
// # What this does not yet isolate
//
// Network access and filesystem writes are NOT blocked. A harness can open a
// socket or write a file, and nothing here stops it. Enforcing that portably
// needs an OS sandbox (seccomp on Linux, a sandbox profile on macOS, or a
// container) and that work is not done.
//
// Until it is, `dore assay` is only as safe as running the implementation
// yourself, and the CLI says so when it runs code from an untrusted source.
// This comment is the specification of a known gap, not an oversight.
package crucible

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"time"
)

// Limits bound a single run.
type Limits struct {
	// Timeout kills the process if it has not exited. Zero means the default.
	Timeout time.Duration
	// MaxOutput caps captured stdout in bytes, so a runaway loop printing
	// forever cannot exhaust memory here. Zero means the default.
	MaxOutput int
}

// DefaultLimits are deliberately tight. A touchstone row is a pure function
// call; anything that takes seconds or prints megabytes is misbehaving.
var DefaultLimits = Limits{
	Timeout:   30 * time.Second,
	MaxOutput: 8 << 20,
}

func (l Limits) withDefaults() Limits {
	if l.Timeout <= 0 {
		l.Timeout = DefaultLimits.Timeout
	}
	if l.MaxOutput <= 0 {
		l.MaxOutput = DefaultLimits.MaxOutput
	}
	return l
}

// Result is what a run produced.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
	// TimedOut reports that the deadline killed the process. Its output is
	// still returned, since rows that completed before the hang are useful.
	TimedOut bool
	Duration time.Duration
}

// ErrNotFound reports that the interpreter is not on PATH.
type ErrNotFound struct {
	Program string
}

func (e *ErrNotFound) Error() string {
	return fmt.Sprintf("%s not found on PATH", e.Program)
}

// Run executes program with args in workDir under the given limits.
//
// The child inherits only PATH and a minimal locale. Nothing else carries
// through: an implementation that reads configuration from the environment
// would behave differently for every person who runs the assay, and a
// touchstone that passes only on your machine is not an oracle.
func Run(ctx context.Context, program string, args []string, workDir string, limits Limits) (*Result, error) {
	limits = limits.withDefaults()

	if _, err := exec.LookPath(program); err != nil {
		return nil, &ErrNotFound{Program: program}
	}

	ctx, cancel := context.WithTimeout(ctx, limits.Timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, program, args...)
	cmd.Dir = workDir
	cmd.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"LC_ALL=C.UTF-8",
		"LANG=C.UTF-8",
		// Determinism: unset, Python randomizes string hashing per process,
		// which can reorder set and dict iteration between runs.
		"PYTHONHASHSEED=0",
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &capped{buf: &stdout, limit: limits.MaxOutput}
	cmd.Stderr = &capped{buf: &stderr, limit: 1 << 20}
	cmd.Stdin = nil

	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)

	res := &Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		Duration: elapsed,
		TimedOut: errors.Is(ctx.Err(), context.DeadlineExceeded),
	}
	if cmd.ProcessState != nil {
		res.ExitCode = cmd.ProcessState.ExitCode()
	}

	// A non-zero exit is data, not an error: the harness may have reported
	// failing rows before exiting. Only a failure to start is an error.
	var exitErr *exec.ExitError
	if err != nil && !errors.As(err, &exitErr) && !res.TimedOut {
		return res, fmt.Errorf("running %s: %w", program, err)
	}
	return res, nil
}

// capped writes to buf until limit bytes, then silently discards. Truncating
// is better than dying: the rows captured before the flood still report.
type capped struct {
	buf   *bytes.Buffer
	limit int
}

func (c *capped) Write(p []byte) (int, error) {
	if remaining := c.limit - c.buf.Len(); remaining > 0 {
		if len(p) > remaining {
			c.buf.Write(p[:remaining])
		} else {
			c.buf.Write(p)
		}
	}
	return len(p), nil
}

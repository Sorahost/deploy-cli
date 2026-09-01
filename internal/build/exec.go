// Package build runs a project's own install and build commands on the
// developer's machine, then assembles the result into a directory that the
// server can serve as-is.
//
// Everything expensive or trusted happens here rather than on the server: the
// deploy agent never sees a package.json, a lockfile, or a build script.
package build

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// ExitError describes a command that ran but failed, carrying enough of its
// output to explain why without making the user re-run with --verbose.
type ExitError struct {
	Command string
	Code    int
	Tail    string
}

func (e *ExitError) Error() string {
	msg := fmt.Sprintf("`%s` failed with exit code %d", e.Command, e.Code)
	if e.Tail != "" {
		return msg + "\n\n" + indent(e.Tail)
	}
	return msg
}

// Runner executes shell commands in a project directory.
type Runner struct {
	Dir    string
	Env    []string  // extra KEY=VALUE entries, appended to the process environment
	Output io.Writer // live output; use io.Discard to keep it quiet
}

// Run executes `command` through the platform's shell.
//
// A shell is used deliberately: build commands come from package.json and
// framework conventions, and they rely on shell behaviour such as `&&`,
// variable expansion and PATH lookup of local binaries. The command string is
// never assembled from network input - it comes from the project's own files.
func (r *Runner) Run(ctx context.Context, command string) error {
	shell, args := shellFor(command)
	cmd := exec.CommandContext(ctx, shell, args...)
	cmd.Dir = r.Dir
	cmd.Env = append(os.Environ(), r.Env...)

	// The tail is kept regardless of whether output is being displayed, so a
	// failure can always explain itself.
	tail := &ringWriter{limit: 8000}
	out := io.Writer(tail)
	if r.Output != nil && r.Output != io.Discard {
		out = io.MultiWriter(tail, r.Output)
	}
	cmd.Stdout = out
	cmd.Stderr = out
	cmd.Stdin = nil

	if err := cmd.Run(); err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return &ExitError{Command: command, Code: exit.ExitCode(), Tail: tail.String()}
		}
		if ctx.Err() != nil {
			return fmt.Errorf("`%s` was cancelled", command)
		}
		return fmt.Errorf("could not run `%s`: %w", command, err)
	}
	return nil
}

// shellFor picks the shell used to interpret a command string.
//
// On Windows this is cmd.exe, because that is what npm scripts are written and
// tested against there; PowerShell would change quoting rules underneath the
// project's own scripts.
func shellFor(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		if comspec := os.Getenv("ComSpec"); comspec != "" {
			return comspec, []string{"/d", "/s", "/c", command}
		}
		return "cmd.exe", []string{"/d", "/s", "/c", command}
	}
	if sh := os.Getenv("SHELL"); sh != "" && strings.HasSuffix(sh, "bash") {
		return sh, []string{"-c", command}
	}
	return "/bin/sh", []string{"-c", command}
}

// ringWriter keeps only the last `limit` bytes written to it.
type ringWriter struct {
	mu    sync.Mutex
	buf   []byte
	limit int
}

func (w *ringWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf = append(w.buf, p...)
	if len(w.buf) > w.limit {
		w.buf = w.buf[len(w.buf)-w.limit:]
	}
	return len(p), nil
}

func (w *ringWriter) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := strings.TrimSpace(string(w.buf))
	// A trimmed tail can start mid-line; drop the partial first line so the
	// excerpt reads cleanly.
	if len(w.buf) == w.limit {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		}
	}
	return s
}

func indent(s string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = "  | " + strings.TrimRight(l, "\r")
	}
	return strings.Join(lines, "\n")
}

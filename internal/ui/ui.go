// Package ui renders everything the CLI prints.
//
// Two rules shape it. First, output has to stay readable when it is not a
// terminal: CI logs, pipes and files get the same words without escape codes or
// redrawn lines. Second, nothing is printed that a reader would have to decode -
// steps are named, durations are shown, and failures say what to do next.
package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// Level controls how much detail is printed.
type Level int

const (
	LevelQuiet Level = iota // errors only
	LevelNormal
	LevelVerbose // includes subprocess output
)

// Printer is the CLI's output surface. The zero value is not usable; use New.
type Printer struct {
	mu      sync.Mutex
	out     io.Writer
	err     io.Writer
	color   bool
	unicode bool
	level   Level
	step    int
	steps   int
}

// New builds a Printer for the given streams.
// `forceColor` and `forceNoColor` come from --color / --no-color; when neither
// is set the decision follows the terminal and the NO_COLOR convention.
func New(out, errw io.Writer, level Level, forceColor, forceNoColor bool) *Printer {
	color := isTerminal(out) && os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb"
	if forceColor {
		color = true
	}
	if forceNoColor {
		color = false
	}
	if color {
		enableVirtualTerminal(out)
	}
	return &Printer{out: out, err: errw, color: color, unicode: color, level: level}
}

func (p *Printer) paint(code, s string) string {
	if !p.color {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func (p *Printer) Bold(s string) string  { return p.paint("1", s) }
func (p *Printer) Dim(s string) string   { return p.paint("90", s) }
func (p *Printer) Green(s string) string { return p.paint("32", s) }
func (p *Printer) Red(s string) string   { return p.paint("31", s) }
func (p *Printer) Cyan(s string) string  { return p.paint("36", s) }
func (p *Printer) Amber(s string) string { return p.paint("33", s) }

func (p *Printer) glyph(fancy, plain string) string {
	if p.unicode {
		return fancy
	}
	return plain
}

func (p *Printer) writef(w io.Writer, format string, args ...any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	fmt.Fprintf(w, format+"\n", args...)
}

// Plan announces how many steps are coming, so each one can be numbered.
func (p *Printer) Plan(n int) {
	p.steps = n
	p.step = 0
}

// Step prints a numbered heading. The returned function ends the step and
// prints how long it took.
func (p *Printer) Step(format string, args ...any) func(detail string) {
	p.step++
	started := time.Now()
	label := fmt.Sprintf(format, args...)
	if p.level > LevelQuiet {
		prefix := p.Dim(fmt.Sprintf("[%d/%d]", p.step, p.steps))
		if p.steps == 0 {
			prefix = p.Dim(p.glyph("→", "-"))
		}
		p.writef(p.out, "%s %s", prefix, p.Bold(label))
	}
	return func(detail string) {
		if p.level == LevelQuiet {
			return
		}
		suffix := p.Dim(fmt.Sprintf("(%s)", took(time.Since(started))))
		if detail != "" {
			suffix = detail + " " + suffix
		}
		p.writef(p.out, "    %s %s", p.Green(p.glyph("✓", "ok")), suffix)
	}
}

// Info prints an unnumbered line at normal verbosity.
func (p *Printer) Info(format string, args ...any) {
	if p.level > LevelQuiet {
		p.writef(p.out, "    %s", fmt.Sprintf(format, args...))
	}
}

// Detail prints dimmed supporting text, e.g. a resolved path.
func (p *Printer) Detail(format string, args ...any) {
	if p.level > LevelQuiet {
		p.writef(p.out, "    %s", p.Dim(fmt.Sprintf(format, args...)))
	}
}

// Warn prints a non-fatal problem. Warnings survive --quiet.
func (p *Printer) Warn(format string, args ...any) {
	p.writef(p.err, "%s %s", p.Amber(p.glyph("!", "!")), fmt.Sprintf(format, args...))
}

// Error prints a failure, plus an optional hint about how to resolve it.
func (p *Printer) Error(err error, hint string) {
	p.writef(p.err, "%s %s", p.Red(p.glyph("✗", "error:")), err.Error())
	if hint != "" {
		p.writef(p.err, "  %s", p.Dim(hint))
	}
}

// Result prints the closing summary of a successful command.
func (p *Printer) Result(lines ...string) {
	if p.level == LevelQuiet {
		return
	}
	p.writef(p.out, "")
	for _, l := range lines {
		p.writef(p.out, "%s", l)
	}
}

// Raw writes a line with no decoration. Used for `--json` and log passthrough.
func (p *Printer) Raw(s string) { p.writef(p.out, "%s", s) }

// Stream returns a writer that prefixes each line of subprocess output.
// At normal verbosity the output is discarded, since a successful build's log
// is noise; on failure the caller reprints the captured tail instead.
func (p *Printer) Stream(prefix string) io.Writer {
	if p.level < LevelVerbose {
		return io.Discard
	}
	return &linePrefixer{p: p, prefix: p.Dim(prefix)}
}

// Verbose reports whether subprocess output is being shown.
func (p *Printer) Verbose() bool { return p.level >= LevelVerbose }

type linePrefixer struct {
	p      *Printer
	prefix string
	buf    strings.Builder
}

func (w *linePrefixer) Write(b []byte) (int, error) {
	for _, c := range b {
		if c == '\n' {
			w.p.writef(w.p.out, "    %s%s", w.prefix, strings.TrimRight(w.buf.String(), "\r"))
			w.buf.Reset()
			continue
		}
		w.buf.WriteByte(c)
	}
	return len(b), nil
}

func took(d time.Duration) string {
	switch {
	case d < time.Second:
		return fmt.Sprintf("%dms", d.Milliseconds())
	case d < time.Minute:
		return fmt.Sprintf("%.1fs", d.Seconds())
	default:
		return fmt.Sprintf("%dm%02ds", int(d.Minutes()), int(d.Seconds())%60)
	}
}

// Bytes formats a size for humans.
func Bytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGT"[exp])
}

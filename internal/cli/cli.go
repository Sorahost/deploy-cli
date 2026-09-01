// Package cli wires the commands together: flag parsing, help, and the
// process-level concerns every command shares.
package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/Sorahost/deploy-cli/internal/ui"
)

// Version is set at build time with -ldflags "-X ...cli.Version=v1.2.3".
var Version = "dev"

// Env is the context a command runs in.
type Env struct {
	Out io.Writer
	Err io.Writer
	P   *ui.Printer

	Dir         string // working directory commands resolve paths against
	JSON        bool   // emit machine-readable output
	AssumeYes   bool   // never prompt; required for CI
	Interactive bool   // stdin is a terminal and prompting is allowed
}

// Command is one subcommand.
type Command struct {
	Name    string
	Summary string
	Usage   string
	Long    string
	Run     func(ctx context.Context, env *Env, args []string) error
	// Alias names that resolve to this command.
	Aliases []string
	// Hidden keeps a command out of the top-level help.
	Hidden bool
}

func commands() []*Command {
	return []*Command{
		cmdLink(),
		cmdDeploy(),
		cmdStatus(),
		cmdLogs(),
		cmdRollback(),
		cmdLogout(),
		cmdVersion(),
	}
}

// Main runs the CLI and returns a process exit code.
//
// Exit codes: 0 success, 1 a command failed, 2 the command line was wrong.
// Keeping those apart is what lets a CI job tell a broken build from a typo.
func Main(args []string) int {
	out, errw := os.Stdout, os.Stderr

	var (
		showHelp    bool
		showVersion bool
		verbose     bool
		quiet       bool
		noColor     bool
		color       bool
		jsonOut     bool
		assumeYes   bool
		dir         string
	)

	fs := flag.NewFlagSet("sorahost", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // errors are reported by this package, not flag's
	fs.BoolVar(&showHelp, "help", false, "")
	fs.BoolVar(&showHelp, "h", false, "")
	fs.BoolVar(&showVersion, "version", false, "")
	fs.BoolVar(&verbose, "verbose", false, "")
	fs.BoolVar(&verbose, "v", false, "")
	fs.BoolVar(&quiet, "quiet", false, "")
	fs.BoolVar(&noColor, "no-color", false, "")
	fs.BoolVar(&color, "color", false, "")
	fs.BoolVar(&jsonOut, "json", false, "")
	fs.BoolVar(&assumeYes, "yes", false, "")
	fs.BoolVar(&assumeYes, "y", false, "")
	fs.StringVar(&dir, "cwd", "", "")

	// Global flags are accepted before the subcommand; anything after it
	// belongs to the subcommand itself.
	global, rest := splitGlobal(args)
	if err := fs.Parse(global); err != nil {
		fmt.Fprintf(errw, "sorahost: %v\n\nRun `sorahost --help` for usage.\n", err)
		return 2
	}

	level := ui.LevelNormal
	switch {
	case quiet, jsonOut:
		level = ui.LevelQuiet
	case verbose:
		level = ui.LevelVerbose
	}
	p := ui.New(out, errw, level, color, noColor)

	if showVersion {
		fmt.Fprintln(out, Version)
		return 0
	}
	if len(rest) == 0 || showHelp && len(rest) == 0 {
		printHelp(out, p)
		if len(rest) == 0 && !showHelp {
			return 2
		}
		return 0
	}

	name := rest[0]
	if strings.HasPrefix(name, "-") {
		fmt.Fprintf(errw, "sorahost: unknown flag %q\n\nRun `sorahost --help` for usage.\n", name)
		return 2
	}
	cmd := find(name)
	if cmd == nil {
		fmt.Fprintf(errw, "sorahost: unknown command %q\n", name)
		if s := suggest(name); s != "" {
			fmt.Fprintf(errw, "Did you mean `sorahost %s`?\n", s)
		}
		fmt.Fprintf(errw, "\nRun `sorahost --help` to see the available commands.\n")
		return 2
	}
	if showHelp {
		printCommandHelp(out, p, cmd)
		return 0
	}

	if dir == "" {
		cwd, err := os.Getwd()
		if err != nil {
			p.Error(err, "")
			return 1
		}
		dir = cwd
	}

	env := &Env{
		Out:         out,
		Err:         errw,
		P:           p,
		Dir:         dir,
		JSON:        jsonOut,
		AssumeYes:   assumeYes || !isInteractive(),
		Interactive: isInteractive() && !assumeYes,
	}

	// Ctrl-C should stop a build or an upload promptly, and leave nothing
	// half-applied: the server only activates a release once it is complete.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := cmd.Run(ctx, env, rest[1:]); err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			p.Warn("cancelled")
			return 1
		}
		if errors.Is(err, errHelpRequested) {
			printCommandHelp(out, p, cmd)
			return 0
		}
		var usage *UsageError
		if errors.As(err, &usage) {
			p.Error(err, "")
			printCommandHelp(errw, p, cmd)
			return 2
		}
		p.Error(err, hintFor(err))
		return 1
	}
	return 0
}

// UsageError marks a command-line mistake, which exits with code 2 and prints
// the command's usage rather than being reported as a failure.
type UsageError struct{ Message string }

func (e *UsageError) Error() string { return e.Message }

func usagef(format string, args ...any) error {
	return &UsageError{Message: fmt.Sprintf(format, args...)}
}

// globalFlags are accepted anywhere on the command line. Requiring them before
// the subcommand is a rule users have no reason to know, and `sorahost deploy -v`
// is what everyone types.
var globalFlags = map[string]bool{
	"help": true, "h": true, "version": true,
	"verbose": true, "v": true, "quiet": true,
	"no-color": true, "color": true, "json": true,
	"yes": true, "y": true,
}

// globalFlagsWithValue take an argument, which must not be mistaken for the
// subcommand or passed on to it.
var globalFlagsWithValue = map[string]bool{"cwd": true}

// splitGlobal separates global flags from the subcommand and its own arguments,
// wherever each appears.
func splitGlobal(args []string) (global, rest []string) {
	seenCommand := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		name, hasValue := flagName(a)

		if name == "" { // not a flag
			if seenCommand {
				rest = append(rest, a)
				continue
			}
			seenCommand = true
			rest = append(rest, a)
			continue
		}
		switch {
		case globalFlagsWithValue[name]:
			global = append(global, a)
			if !hasValue && i+1 < len(args) {
				i++
				global = append(global, args[i])
			}
		case globalFlags[name]:
			global = append(global, a)
		default:
			// Unknown flags belong to the subcommand, which reports on them.
			rest = append(rest, a)
		}
	}
	return global, rest
}

// flagName extracts the name from `-x`, `--x` or `--x=value`.
// It returns "" for arguments that are not flags.
func flagName(arg string) (name string, hasValue bool) {
	if len(arg) < 2 || arg[0] != '-' || arg == "--" {
		return "", false
	}
	trimmed := strings.TrimLeft(arg, "-")
	if eq := strings.IndexByte(trimmed, '='); eq >= 0 {
		return trimmed[:eq], true
	}
	return trimmed, false
}

func find(name string) *Command {
	for _, c := range commands() {
		if c.Name == name {
			return c
		}
		for _, a := range c.Aliases {
			if a == name {
				return c
			}
		}
	}
	return nil
}

// suggest offers the closest command name for a typo.
func suggest(name string) string {
	best, bestScore := "", 3 // only suggest genuinely close matches
	for _, c := range commands() {
		if d := distance(name, c.Name); d < bestScore {
			best, bestScore = c.Name, d
		}
	}
	return best
}

// distance is the Levenshtein edit distance between two short strings.
func distance(a, b string) int {
	prev := make([]int, len(b)+1)
	curr := make([]int, len(b)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(a); i++ {
		curr[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			curr[j] = min(min(curr[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}
	return prev[len(b)]
}

func printHelp(w io.Writer, p *ui.Printer) {
	fmt.Fprintf(w, "%s\n\n", p.Bold("sorahost - build locally, deploy to a SORAHOST server"))
	fmt.Fprintf(w, "%s\n  sorahost [global flags] <command> [flags]\n\n", p.Bold("USAGE"))

	fmt.Fprintf(w, "%s\n", p.Bold("COMMANDS"))
	list := commands()
	for _, c := range list {
		if c.Hidden {
			continue
		}
		fmt.Fprintf(w, "  %-12s %s\n", c.Name, c.Summary)
	}

	fmt.Fprintf(w, "\n%s\n", p.Bold("GLOBAL FLAGS"))
	for _, f := range [][2]string{
		{"--cwd <dir>", "run as if started in <dir>"},
		{"-v, --verbose", "show install and build output"},
		{"--quiet", "print errors only"},
		{"--json", "emit machine-readable output (implies --quiet)"},
		{"-y, --yes", "never prompt; required in CI"},
		{"--color/--no-color", "force colour on or off"},
		{"--version", "print the CLI version"},
	} {
		fmt.Fprintf(w, "  %-20s %s\n", f[0], f[1])
	}

	fmt.Fprintf(w, "\n%s\n", p.Bold("GETTING STARTED"))
	fmt.Fprintf(w, "  %s\n", p.Dim("# once per project, using the endpoint and token from the server console"))
	fmt.Fprintf(w, "  sorahost link\n")
	fmt.Fprintf(w, "  %s\n", p.Dim("# every time you want to publish"))
	fmt.Fprintf(w, "  sorahost deploy\n\n")
	fmt.Fprintf(w, "  %s\n", p.Dim("Run `sorahost <command> --help` for details on a command."))
}

func printCommandHelp(w io.Writer, p *ui.Printer, c *Command) {
	fmt.Fprintf(w, "%s\n\n", p.Bold("sorahost "+c.Name+" - "+c.Summary))
	fmt.Fprintf(w, "%s\n  %s\n", p.Bold("USAGE"), c.Usage)
	if len(c.Aliases) > 0 {
		fmt.Fprintf(w, "\n%s\n  %s\n", p.Bold("ALIASES"), strings.Join(c.Aliases, ", "))
	}
	if c.Long != "" {
		fmt.Fprintf(w, "\n%s\n", strings.TrimRight(c.Long, "\n"))
	}
}

func cmdVersion() *Command {
	return &Command{
		Name:    "version",
		Summary: "print the CLI version",
		Usage:   "sorahost version",
		Run: func(_ context.Context, env *Env, _ []string) error {
			fmt.Fprintln(env.Out, Version)
			return nil
		},
	}
}

// newFlagSet builds a flag set that reports errors through UsageError rather
// than printing its own inconsistent usage text.
func newFlagSet(name string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	return fs
}

func parseFlags(fs *flag.FlagSet, args []string) error {
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return errHelpRequested
		}
		return usagef("%v", err)
	}
	return nil
}

// errHelpRequested is returned when a subcommand was given -h/--help, so the
// caller prints that command's help and exits successfully.
var errHelpRequested = errors.New("help requested")

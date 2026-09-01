package cli

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Sorahost/deploy-cli/internal/api"
	"github.com/Sorahost/deploy-cli/internal/build"
	"github.com/Sorahost/deploy-cli/internal/config"
	"github.com/Sorahost/deploy-cli/internal/ui"
)

func asExitError(err error, target **build.ExitError) bool {
	return errors.As(err, target)
}

// --- status -----------------------------------------------------------------

func cmdStatus() *Command {
	return &Command{
		Name:    "status",
		Summary: "show what is currently deployed",
		Usage:   "sorahost status [--endpoint URL]",
		Long: `Reports the live release, how it is being served, and the releases available
to roll back to.

FLAGS
  --endpoint URL   query this server instead of the linked one`,
		Run: runStatus,
	}
}

func runStatus(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet("status")
	endpointFlag := fs.String("endpoint", "", "")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	endpoint, token, _, err := resolveTarget(env, *endpointFlag)
	if err != nil {
		return err
	}
	client, err := newClient(endpoint, token)
	if err != nil {
		return err
	}
	st, err := client.Status(ctx)
	if err != nil {
		return err
	}

	if env.JSON {
		return writeJSON(env, st)
	}

	p := env.P
	serving := p.Red("not serving")
	if st.Running {
		serving = p.Green("serving")
	}
	rows := [][2]string{
		{"State", serving},
		{"Release", orDash(p, st.Release)},
		{"Mode", orDash(p, st.Mode)},
		{"Framework", orDash(p, st.Framework)},
		{"Since", orDash(p, humanTime(st.StartedAt))},
		{"Releases", fmt.Sprintf("%d stored", len(st.Releases))},
	}
	for _, r := range rows {
		fmt.Fprintf(env.Out, "  %-11s %s\n", p.Dim(r[0]), r[1])
	}

	if len(st.History) > 0 {
		fmt.Fprintf(env.Out, "\n  %s\n", p.Dim("recent deployments"))
		for i, h := range st.History {
			if i >= 5 {
				break
			}
			marker := " "
			if h.ID == st.Release {
				marker = p.Green("*")
			}
			fmt.Fprintf(env.Out, "  %s %s  %s  %s\n", marker, h.ID, p.Dim(h.Mode), p.Dim(humanTime(h.At)))
		}
	}
	return nil
}

// --- logs -------------------------------------------------------------------

func cmdLogs() *Command {
	return &Command{
		Name:    "logs",
		Summary: "print the server's recent log output",
		Usage:   "sorahost logs [-n LINES] [-f] [--endpoint URL]",
		Long: `Prints the deploy agent's in-memory log buffer, which covers boot, deployments
and request logging for the running release.

FLAGS
  -n LINES         how many lines to show (default 100, max 5000)
  -f, --follow     keep polling for new lines until interrupted
  --endpoint URL   query this server instead of the linked one`,
		Run: runLogs,
	}
}

func runLogs(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet("logs")
	tail := fs.Int("n", 100, "")
	follow := fs.Bool("follow", false, "")
	fs.BoolVar(follow, "f", false, "")
	endpointFlag := fs.String("endpoint", "", "")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if *tail < 1 || *tail > 5000 {
		return usagef("-n must be between 1 and 5000")
	}

	endpoint, token, _, err := resolveTarget(env, *endpointFlag)
	if err != nil {
		return err
	}
	client, err := newClient(endpoint, token)
	if err != nil {
		return err
	}

	lines, err := client.Logs(ctx, *tail)
	if err != nil {
		return err
	}
	if env.JSON {
		return writeJSON(env, lines)
	}
	for _, l := range lines {
		printLog(env, l)
	}
	if !*follow {
		return nil
	}

	// The agent has no streaming endpoint, so following is polling with the
	// last line we printed used to skip what we have already seen.
	seen := ""
	if len(lines) > 0 {
		seen = key(lines[len(lines)-1])
	}
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
		}
		batch, err := client.Logs(ctx, 200)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			env.P.Warn("%v", err)
			continue
		}
		for i, l := range batch {
			if seen != "" && key(l) == seen {
				batch = batch[i+1:]
				break
			}
		}
		for _, l := range batch {
			printLog(env, l)
			seen = key(l)
		}
	}
}

func key(l api.LogLine) string { return l.TS + "\x00" + l.Message }

func printLog(env *Env, l api.LogLine) {
	p := env.P
	tag := strings.ToUpper(l.Tag)
	paint := p.Dim
	switch l.Tag {
	case "error":
		paint = p.Red
	case "warn":
		paint = p.Amber
	case "ready", "http":
		paint = p.Green
	case "boot":
		paint = p.Cyan
	}
	fmt.Fprintf(env.Out, "%s %s %s\n", p.Dim(shortTime(l.TS)), paint(fmt.Sprintf("%-7s", tag)), l.Message)
}

// --- rollback ---------------------------------------------------------------

func cmdRollback() *Command {
	return &Command{
		Name:    "rollback",
		Summary: "activate a previous release",
		Usage:   "sorahost rollback [RELEASE_ID] [--endpoint URL]",
		Long: `Switches the server back to an earlier release. With no argument it activates
the release immediately before the current one.

Run ` + "`sorahost status`" + ` to see which release ids are available.`,
		Run: runRollback,
	}
}

func runRollback(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet("rollback")
	endpointFlag := fs.String("endpoint", "", "")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 1 {
		return usagef("expected at most one release id")
	}
	target := fs.Arg(0)

	endpoint, token, _, err := resolveTarget(env, *endpointFlag)
	if err != nil {
		return err
	}
	client, err := newClient(endpoint, token)
	if err != nil {
		return err
	}

	what := "the previous release"
	if target != "" {
		what = target
	}
	ok, err := confirm(env, fmt.Sprintf("Roll back to %s?", what))
	if err != nil {
		return err
	}
	if !ok {
		env.P.Warn("cancelled")
		return nil
	}

	done := env.P.Step("Rolling back")
	release, err := client.Rollback(ctx, target)
	if err != nil {
		return err
	}
	done("")

	if env.JSON {
		return writeJSON(env, map[string]string{"release": release})
	}
	env.P.Result(fmt.Sprintf("%s now serving %s", env.P.Green("✓"), env.P.Bold(release)))
	return nil
}

// --- logout -----------------------------------------------------------------

func cmdLogout() *Command {
	return &Command{
		Name:    "logout",
		Summary: "forget a stored deploy token",
		Usage:   "sorahost logout [--endpoint URL] [--all]",
		Long: `Removes a deploy token from this machine's credential store.

The token itself remains valid on the server. To actually revoke it, run
` + "`token rotate`" + ` in the server console.

FLAGS
  --endpoint URL   forget this endpoint instead of the linked one
  --all            forget every stored token`,
		Run: runLogout,
	}
}

func runLogout(_ context.Context, env *Env, args []string) error {
	fs := newFlagSet("logout")
	endpointFlag := fs.String("endpoint", "", "")
	all := fs.Bool("all", false, "")
	if err := parseFlags(fs, args); err != nil {
		return err
	}

	creds, err := config.LoadCredentials()
	if err != nil {
		return err
	}

	if *all {
		endpoints := creds.Endpoints()
		if len(endpoints) == 0 {
			env.P.Warn("no tokens are stored")
			return nil
		}
		ok, cerr := confirm(env, fmt.Sprintf("Forget all %d stored tokens?", len(endpoints)))
		if cerr != nil {
			return cerr
		}
		if !ok {
			return nil
		}
		for _, e := range endpoints {
			if err := creds.Forget(e); err != nil {
				return err
			}
		}
		env.P.Result(fmt.Sprintf("%s forgot %d token(s)", env.P.Green("✓"), len(endpoints)))
		return nil
	}

	endpoint := *endpointFlag
	if endpoint == "" {
		project, perr := config.FindProject(env.Dir)
		if perr != nil {
			return &ErrNeedsInput{What: "an endpoint", How: "Pass --endpoint, or run this from a linked project."}
		}
		endpoint = project.Endpoint
	}
	if err := creds.Forget(endpoint); err != nil {
		return err
	}
	env.P.Result(
		fmt.Sprintf("%s forgot the token for this server", env.P.Green("✓")),
		env.P.Dim("  Run `token rotate` in the server console to revoke it there too."),
	)
	return nil
}

// --- shared formatting ------------------------------------------------------

func orDash(p *ui.Printer, s string) string {
	if s == "" {
		return p.Dim("-")
	}
	return s
}

// humanTime renders an RFC3339 timestamp as a relative age, which is what a
// reader actually wants to know about a deployment.
func humanTime(ts string) string {
	if ts == "" {
		return ""
	}
	t, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return ts
	}
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%d minutes ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%d hours ago", int(d.Hours()))
	default:
		return fmt.Sprintf("%d days ago", int(d.Hours()/24))
	}
}

func shortTime(ts string) string {
	if len(ts) >= 19 {
		return ts[11:19]
	}
	return ts
}

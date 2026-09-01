package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Sorahost/deploy-cli/internal/api"
	"github.com/Sorahost/deploy-cli/internal/config"
	"github.com/Sorahost/deploy-cli/internal/detect"
)

func cmdLink() *Command {
	return &Command{
		Name:    "link",
		Aliases: []string{"login"},
		Summary: "connect this project to a server",
		Usage:   "sorahost link [--endpoint URL] [--token TOKEN] [--name NAME] [--force]",
		Long: `Stores the deploy token for a server and writes ` + config.ProjectFile + ` in the
current directory. Both values are printed by the server console when it starts;
run ` + "`url`" + ` and ` + "`token rotate`" + ` there if you no longer have them.

The token is written to your user config directory, never to ` + config.ProjectFile + `,
so the project file is safe to commit.

FLAGS
  --endpoint URL   the deploy API URL, including its random path segment
  --token TOKEN    the deploy token (prompted for if omitted)
  --name NAME      a label for this project, used in CLI output
  --force          overwrite an existing ` + config.ProjectFile + `

NON-INTERACTIVE USE
  Set ` + config.EnvEndpoint + ` and ` + config.EnvToken + ` instead of running this
  command; CI does not need a linked project file.

EXAMPLES
  sorahost link
  sorahost link --endpoint https://app.example.com/_sorahost/AbC123 --token shd_...`,
		Run: runLink,
	}
}

func runLink(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet("link")
	endpoint := fs.String("endpoint", "", "")
	token := fs.String("token", "", "")
	name := fs.String("name", "", "")
	force := fs.Bool("force", false, "")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usagef("unexpected argument %q", fs.Arg(0))
	}

	p := env.P
	p.Plan(3)

	// --- endpoint -----------------------------------------------------------
	value := strings.TrimSpace(*endpoint)
	if value == "" {
		value = strings.TrimSpace(os.Getenv(config.EnvEndpoint))
	}
	if value == "" {
		if existing, err := config.FindProject(env.Dir); err == nil {
			value = existing.Endpoint
		}
	}
	if value == "" {
		answer, err := ask(env, "Deploy endpoint URL", "")
		if err != nil {
			return needEndpoint(err)
		}
		value = strings.TrimSpace(answer)
	}
	if err := config.ValidateEndpoint(value); err != nil {
		return err
	}

	// --- token --------------------------------------------------------------
	secret := strings.TrimSpace(*token)
	if secret != "" {
		p.Warn("passing --token puts the token in your shell history; prefer the prompt or %s", config.EnvToken)
	}
	if secret == "" {
		secret = strings.TrimSpace(os.Getenv(config.EnvToken))
	}
	if secret == "" {
		answer, err := askSecret(env, "Deploy token")
		if err != nil {
			return needToken(err)
		}
		secret = answer
	}
	if secret == "" {
		return &ErrNeedsInput{What: "a deploy token", How: "Run `token rotate` in the server console to issue one."}
	}

	// --- verify -------------------------------------------------------------
	done := p.Step("Verifying credentials")
	client, err := api.New(value, secret, "sorahost/"+Version)
	if err != nil {
		return err
	}
	pong, err := client.Ping(ctx)
	if err != nil {
		return err
	}
	done(p.Dim("agent " + pong.Agent))

	// --- store --------------------------------------------------------------
	done = p.Step("Saving the token")
	creds, err := config.LoadCredentials()
	if err != nil {
		return err
	}
	if err := creds.Set(value, secret); err != nil {
		return fmt.Errorf("could not save the token: %w", err)
	}
	done(p.Dim(creds.Path()))

	// --- project file -------------------------------------------------------
	done = p.Step("Writing %s", config.ProjectFile)
	path := filepath.Join(env.Dir, config.ProjectFile)
	if _, err := os.Stat(path); err == nil && !*force {
		ok, cerr := confirm(env, fmt.Sprintf("%s already exists. Overwrite it?", config.ProjectFile))
		if cerr != nil {
			return cerr
		}
		if !ok {
			p.Warn("kept the existing %s; the token was still saved", config.ProjectFile)
			return nil
		}
	}

	project := &config.Project{Endpoint: value, Name: *name}
	if project.Name == "" {
		project.Name = filepath.Base(env.Dir)
	}
	if err := project.Save(path); err != nil {
		return err
	}
	done("")

	// Detection is only reported, never written: recording guesses in the
	// project file would freeze them, and they are cheap to redo each deploy.
	summary := ""
	if plan, derr := detect.Detect(env.Dir); derr == nil {
		summary = fmt.Sprintf("%s (%s)", plan.Framework, plan.Mode)
	}

	if env.JSON {
		return writeJSON(env, map[string]any{"endpoint": value, "project": path, "detected": summary})
	}
	lines := []string{
		fmt.Sprintf("%s linked %s", p.Green("✓"), p.Bold(project.Name)),
	}
	if summary != "" {
		lines = append(lines, fmt.Sprintf("  detected %s", p.Bold(summary)))
	}
	lines = append(lines, "", p.Dim("Next: run `sorahost deploy` to build and publish."))
	p.Result(lines...)
	return nil
}

func needEndpoint(err error) error {
	var needs *ErrNeedsInput
	if ok := asNeedsInput(err, &needs); ok {
		needs.What = "an endpoint URL"
		needs.How = "Pass --endpoint, or set " + config.EnvEndpoint + "."
		return needs
	}
	return err
}

func needToken(err error) error {
	var needs *ErrNeedsInput
	if ok := asNeedsInput(err, &needs); ok {
		needs.What = "a deploy token"
		needs.How = "Set " + config.EnvToken + ", or run this command in a terminal."
		return needs
	}
	return err
}

func writeJSON(env *Env, payload any) error {
	enc := json.NewEncoder(env.Out)
	enc.SetIndent("", "  ")
	return enc.Encode(payload)
}

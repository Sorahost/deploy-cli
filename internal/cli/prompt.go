package cli

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/Sorahost/deploy-cli/internal/api"
	"github.com/Sorahost/deploy-cli/internal/config"
)

// isInteractive reports whether it is reasonable to ask the user a question.
// In CI, stdin is not a terminal and every prompt must instead be an error that
// names the flag or environment variable to set.
func isInteractive() bool {
	return term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd()))
}

// ErrNeedsInput is returned when a value is missing and cannot be prompted for.
type ErrNeedsInput struct {
	What string
	How  string
}

func (e *ErrNeedsInput) Error() string {
	return fmt.Sprintf("%s is required", e.What)
}

// ask reads a line of visible input.
func ask(env *Env, question, fallback string) (string, error) {
	if !env.Interactive {
		return "", &ErrNeedsInput{What: question}
	}
	suffix := ": "
	if fallback != "" {
		suffix = fmt.Sprintf(" [%s]: ", fallback)
	}
	fmt.Fprintf(env.Out, "%s%s", question, suffix)

	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return "", fmt.Errorf("could not read input: %w", err)
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return fallback, nil
	}
	return line, nil
}

// askSecret reads a line without echoing it, so a token never lands in the
// terminal's scrollback or the shell's history.
func askSecret(env *Env, question string) (string, error) {
	if !env.Interactive {
		return "", &ErrNeedsInput{What: question}
	}
	fmt.Fprintf(env.Out, "%s: ", question)
	raw, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(env.Out)
	if err != nil {
		return "", fmt.Errorf("could not read input: %w", err)
	}
	return strings.TrimSpace(string(raw)), nil
}

// confirm asks a yes/no question. With --yes it answers itself.
func confirm(env *Env, question string) (bool, error) {
	if env.AssumeYes {
		return true, nil
	}
	answer, err := ask(env, question+" [y/N]", "n")
	if err != nil {
		return false, err
	}
	a := strings.ToLower(answer)
	return a == "y" || a == "yes", nil
}

// hintFor turns a few well-known failures into the next thing to try.
func hintFor(err error) string {
	var apiErr *api.Error
	if errors.As(err, &apiErr) {
		return apiErr.Hint()
	}
	var needs *ErrNeedsInput
	if errors.As(err, &needs) {
		if needs.How != "" {
			return needs.How
		}
		return "This is a non-interactive session; pass the value as a flag or set it in the environment."
	}
	if errors.Is(err, config.ErrNoProject) {
		return "Run `sorahost link` in the project you want to deploy."
	}
	return ""
}

// resolveTarget finds the endpoint and token for a command.
//
// Precedence is: explicit flag, then the environment (for CI), then the
// project file plus the credential store. The token is never read from the
// project file - see internal/config.
func resolveTarget(env *Env, endpointFlag string) (endpoint, token string, project *config.Project, err error) {
	endpoint = strings.TrimSpace(endpointFlag)
	if endpoint == "" {
		endpoint = strings.TrimSpace(os.Getenv(config.EnvEndpoint))
	}

	if p, perr := config.FindProject(env.Dir); perr == nil {
		project = p
		if endpoint == "" {
			endpoint = p.Endpoint
		}
	} else if !errors.Is(perr, config.ErrNoProject) {
		return "", "", nil, perr
	}

	if endpoint == "" {
		return "", "", nil, config.ErrNoProject
	}
	if err := config.ValidateEndpoint(endpoint); err != nil {
		return "", "", nil, err
	}

	creds, err := config.LoadCredentials()
	if err != nil {
		return "", "", nil, err
	}
	token, ok := creds.Token(endpoint)
	if !ok {
		return "", "", nil, &ErrNeedsInput{
			What: "a deploy token for this server",
			How:  "Run `sorahost link` to store one, or set " + config.EnvToken + " for CI.",
		}
	}
	return endpoint, token, project, nil
}

// newClient builds an API client for the resolved target.
func newClient(endpoint, token string) (*api.Client, error) {
	return api.New(endpoint, token, "sorahost/"+Version)
}

// asNeedsInput reports whether err is an ErrNeedsInput, binding it to target.
func asNeedsInput(err error, target **ErrNeedsInput) bool {
	return errors.As(err, target)
}

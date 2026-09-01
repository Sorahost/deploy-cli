package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Sorahost/deploy-cli/internal/artifact"
	"github.com/Sorahost/deploy-cli/internal/build"
	"github.com/Sorahost/deploy-cli/internal/config"
	"github.com/Sorahost/deploy-cli/internal/detect"
	"github.com/Sorahost/deploy-cli/internal/ui"
)

func cmdDeploy() *Command {
	return &Command{
		Name:    "deploy",
		Summary: "build the project locally and publish it",
		Usage:   "sorahost deploy [flags]",
		Long: `Installs dependencies, runs the project's build, packs the result into an
artifact, and uploads it. The server verifies the artifact, unpacks it into a new
release and switches over; if the new release does not start, it rolls back to
the previous one on its own.

Nothing is installed or built on the server. Everything that runs project code
runs here, on your machine.

FLAGS
  --skip-install     do not install dependencies (assumes node_modules is current)
  --skip-build       do not run the build (assumes the output directory is current)
  --dry-run          build and pack, but do not upload
  --artifact PATH    also write the packed artifact to PATH
  --endpoint URL     deploy to this server instead of the linked one

NON-INTERACTIVE USE
  Set ` + config.EnvEndpoint + ` and ` + config.EnvToken + `; no project file is needed.

EXAMPLES
  sorahost deploy
  sorahost deploy --skip-install -v
  sorahost deploy --dry-run --artifact ./build.tgz`,
		Run: runDeploy,
	}
}

func runDeploy(ctx context.Context, env *Env, args []string) error {
	fs := newFlagSet("deploy")
	skipInstall := fs.Bool("skip-install", false, "")
	skipBuild := fs.Bool("skip-build", false, "")
	dryRun := fs.Bool("dry-run", false, "")
	artifactOut := fs.String("artifact", "", "")
	endpointFlag := fs.String("endpoint", "", "")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usagef("unexpected argument %q", fs.Arg(0))
	}

	p := env.P

	// Credentials are resolved first: finding out the token is missing after a
	// five-minute build is a bad way to learn it.
	var endpoint, token string
	var project *config.Project
	if !*dryRun {
		var err error
		endpoint, token, project, err = resolveTarget(env, *endpointFlag)
		if err != nil {
			return err
		}
	} else if pj, err := config.FindProject(env.Dir); err == nil {
		project = pj
	}

	root := env.Dir
	if project != nil {
		root = project.Dir()
	}

	plan, err := planFor(root, project)
	if err != nil {
		return err
	}

	steps := 4
	if *dryRun {
		steps = 3
	}
	if plan.Install == "" || *skipInstall {
		steps--
	}
	if plan.Build == "" || *skipBuild {
		steps--
	}
	p.Plan(steps)
	p.Detail("%s %s  %s %s", "project", p.Bold(root), "target", targetLabel(p, endpoint, *dryRun))
	p.Detail("%s %s (%s), package manager %s", "detected", p.Bold(plan.Framework), plan.Mode, plan.PackageManager)

	runner := &build.Runner{Dir: root, Env: plan.PackageManager.Env(), Output: p.Stream(string(plan.PackageManager) + " | ")}

	// --- install ------------------------------------------------------------
	if plan.Install != "" && !*skipInstall {
		done := p.Step("Installing dependencies")
		if err := runner.Run(ctx, plan.Install); err != nil {
			return withBuildHint(err, p)
		}
		done("")
	} else if *skipInstall {
		p.Detail("skipping dependency install (--skip-install)")
	}

	// --- build --------------------------------------------------------------
	if plan.Build != "" && !*skipBuild {
		done := p.Step("Building (%s)", plan.Build)
		if err := runner.Run(ctx, plan.Build); err != nil {
			return withBuildHint(err, p)
		}
		done("")
	} else if *skipBuild {
		p.Detail("skipping build (--skip-build)")
	}

	// --- package ------------------------------------------------------------
	done := p.Step("Packaging artifact")
	work, err := os.MkdirTemp("", "sorahost-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(work)

	stageDir := filepath.Join(work, "stage")
	if err := os.MkdirAll(stageDir, 0o755); err != nil {
		return err
	}

	req := &build.Request{
		Root:    root,
		Plan:    plan,
		StageIn: stageDir,
		Version: Version,
		Runner:  runner,
		Log:     func(format string, a ...any) { p.Detail(format, a...) },
		Warn:    func(format string, a ...any) { p.Warn(format, a...) },
	}
	if project != nil {
		req.Vars = project.Vars
		req.Env = project.Env
		req.CompatibilityDate = project.CompatibilityDate
		req.CompatibilityFlags = project.CompatibilityFlags
	}

	staged, err := build.Stage(ctx, req)
	if err != nil {
		return err
	}

	archivePath := filepath.Join(work, "artifact.tgz")
	archive, err := artifact.Pack(stageDir, archivePath)
	if err != nil {
		return err
	}
	done(fmt.Sprintf("%s in %d files, %s compressed",
		p.Bold(ui.Bytes(staged.Bytes)), staged.Files, p.Bold(ui.Bytes(archive.Size))))

	if *artifactOut != "" {
		if err := copyOut(archivePath, *artifactOut); err != nil {
			return fmt.Errorf("could not write %s: %w", *artifactOut, err)
		}
		p.Detail("artifact written to %s", *artifactOut)
	}

	if *dryRun {
		p.Result(fmt.Sprintf("%s built %s artifact, not uploaded (--dry-run)",
			p.Green("✓"), p.Bold(staged.Manifest.Mode)),
			p.Dim("  sha256 "+archive.SHA256))
		return nil
	}

	// --- upload -------------------------------------------------------------
	done = p.Step("Uploading and activating")
	client, err := newClient(endpoint, token)
	if err != nil {
		return err
	}

	progress := uploadProgress(p, archive.Size)
	result, err := client.Deploy(ctx, archivePath, archive.SHA256, progress)
	if err != nil {
		return err
	}
	done("")

	if env.JSON {
		return writeJSON(env, map[string]any{
			"release":   result.Release,
			"mode":      result.Mode,
			"framework": result.Framework,
			"bytes":     archive.Size,
			"sha256":    archive.SHA256,
		})
	}
	p.Result(
		fmt.Sprintf("%s deployed %s", p.Green("✓"), p.Bold(result.Release)),
		fmt.Sprintf("  %s %s, %s", p.Dim("serving"), result.Framework, result.Mode),
		"",
		p.Dim("  sorahost status   see what is live"),
		p.Dim("  sorahost logs -f  follow the server log"),
	)
	return nil
}

// planFor merges detection with any overrides in the project file. Overrides
// always win: detection is a starting point, not a contract.
func planFor(root string, project *config.Project) (*detect.Plan, error) {
	plan, err := detect.Detect(root)
	if err != nil {
		// An explicit mode in the project file is enough to proceed without
		// detection, which is how unusual projects opt out entirely.
		if project == nil || project.Mode == "" {
			return nil, err
		}
		plan = &detect.Plan{Framework: "custom", PackageManager: detect.NPM}
	}
	if project == nil {
		return plan, nil
	}

	if project.Framework != "" {
		plan.Framework = project.Framework
	}
	if project.Mode != "" {
		plan.Mode = detect.Mode(project.Mode)
		// A mode the project chose invalidates a self-contained layout guessed
		// for a different one.
		plan.SelfContained = ""
	}
	if project.Install != "" {
		plan.Install = project.Install
	}
	if project.Build != "" {
		plan.Build = project.Build
	}
	if project.Start != "" {
		plan.Start = project.Start
	}
	if project.Output != "" {
		plan.Output = project.Output
	}
	if project.Entry != "" {
		plan.Entry = project.Entry
	}
	if project.SPA != nil {
		plan.SPA = *project.SPA
	}

	switch plan.Mode {
	case detect.ModeNode:
		if plan.Start == "" {
			return nil, fmt.Errorf(`"mode": "node" needs a "startCommand" in %s`, config.ProjectFile)
		}
	case detect.ModeStatic:
		if plan.Output == "" {
			return nil, fmt.Errorf(`"mode": "static" needs an "outputDirectory" in %s`, config.ProjectFile)
		}
	case detect.ModeWorker:
		if plan.Entry == "" {
			return nil, fmt.Errorf(`"mode": "worker" needs an "entry" in %s`, config.ProjectFile)
		}
	}
	return plan, nil
}

// uploadProgress renders a single rewritten line on a terminal, and periodic
// milestones everywhere else so CI logs stay short but not silent.
func uploadProgress(p *ui.Printer, total int64) func(sent, total int64) {
	if !p.Verbose() && total < 4<<20 {
		return nil
	}
	var lastPercent int64 = -1
	return func(sent, total int64) {
		if total == 0 {
			return
		}
		percent := sent * 100 / total
		if percent/10 == lastPercent/10 && percent < 100 {
			return
		}
		lastPercent = percent
		p.Detail("uploaded %s of %s (%d%%)", ui.Bytes(sent), ui.Bytes(total), percent)
	}
}

func targetLabel(p *ui.Printer, endpoint string, dryRun bool) string {
	if dryRun {
		return p.Dim("(dry run)")
	}
	// The path segment is a secret, so only the host is ever printed.
	if i := strings.Index(endpoint, "://"); i >= 0 {
		rest := endpoint[i+3:]
		if j := strings.IndexByte(rest, '/'); j >= 0 {
			return p.Bold(endpoint[:i+3] + rest[:j])
		}
	}
	return p.Bold(endpoint)
}

// withBuildHint adds the advice that resolves most install and build failures.
func withBuildHint(err error, p *ui.Printer) error {
	var exit *build.ExitError
	if !asExitError(err, &exit) {
		return err
	}
	if p.Verbose() {
		return err
	}
	return fmt.Errorf("%w\n\n       Run again with -v to see the full output", err)
}

func copyOut(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

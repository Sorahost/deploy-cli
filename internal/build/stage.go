package build

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Sorahost/deploy-cli/internal/detect"
)

// ManifestName is the file inside an artifact that tells the server how to
// serve it. Its schema is validated again on the server side.
const ManifestName = "sorahost.json"

// Manifest is the deployment plan that travels inside the artifact.
type Manifest struct {
	Version   int    `json:"version"`
	Mode      string `json:"mode"`
	Framework string `json:"framework,omitempty"`

	Entry string `json:"entry,omitempty"` // worker mode: bundled module
	Dir   string `json:"dir,omitempty"`   // static mode: assets; node mode: app root
	Start string `json:"start,omitempty"` // node mode: command to run
	SPA   bool   `json:"spa,omitempty"`

	Vars map[string]any `json:"vars,omitempty"`
	Env  []string       `json:"env,omitempty"`

	CompatibilityDate  string   `json:"compatibilityDate,omitempty"`
	CompatibilityFlags []string `json:"compatibilityFlags,omitempty"`

	BuiltBy string `json:"builtBy"`
	BuiltAt string `json:"builtAt"`
}

// Request is everything Stage needs to assemble an artifact.
type Request struct {
	Root    string       // project root
	Plan    *detect.Plan // how to build and serve
	StageIn string       // empty directory to assemble into

	Vars               map[string]any
	Env                []string
	CompatibilityDate  string
	CompatibilityFlags []string

	Version string // CLI version, recorded in the manifest
	Runner  *Runner
	Log     func(format string, args ...any)
	// Warn reports something the user should know but that does not stop the
	// build - most importantly, secrets that were left out of the artifact.
	Warn func(format string, args ...any)
}

// Result describes what was staged.
type Result struct {
	Manifest Manifest
	Files    int
	Bytes    int64
}

// Stage lays out the artifact contents in `req.StageIn`.
//
// It assumes install and build have already run; see Install and Compile. The
// layout is deliberately boring - `worker.js`, `public/`, or `app/` plus a
// manifest - so that the server needs no knowledge of any framework.
func Stage(ctx context.Context, req *Request) (*Result, error) {
	m := Manifest{
		Version:            1,
		Mode:               string(req.Plan.Mode),
		Framework:          req.Plan.Framework,
		Vars:               req.Vars,
		Env:                req.Env,
		CompatibilityDate:  req.CompatibilityDate,
		CompatibilityFlags: req.CompatibilityFlags,
		BuiltBy:            "sorahost/" + req.Version,
		BuiltAt:            time.Now().UTC().Format(time.RFC3339),
	}

	var err error
	switch req.Plan.Mode {
	case detect.ModeWorker:
		err = stageWorker(req, &m)
	case detect.ModeStatic:
		err = stageStatic(req, &m)
	case detect.ModeNode:
		err = stageNode(ctx, req, &m)
	default:
		err = fmt.Errorf("unsupported mode %q", req.Plan.Mode)
	}
	if err != nil {
		return nil, err
	}

	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(req.StageIn, ManifestName), append(raw, '\n'), 0o644); err != nil {
		return nil, err
	}

	files, bytes, err := measure(req.StageIn)
	if err != nil {
		return nil, err
	}
	return &Result{Manifest: m, Files: files, Bytes: bytes}, nil
}

func stageWorker(req *Request, m *Manifest) error {
	entry := filepath.Join(req.Root, filepath.FromSlash(req.Plan.Entry))
	if _, err := os.Stat(entry); err != nil {
		return fmt.Errorf("worker entry %s does not exist", req.Plan.Entry)
	}
	if err := BundleWorker(entry, filepath.Join(req.StageIn, "worker.js"), true); err != nil {
		return err
	}
	m.Entry = "worker.js"

	// A Workers project may also ship static assets alongside its module.
	if req.Plan.Output != "" {
		src := filepath.Join(req.Root, filepath.FromSlash(req.Plan.Output))
		if isDir(src) {
			if err := copyTree(src, filepath.Join(req.StageIn, "public"), nil); err != nil {
				return err
			}
			m.Dir = "public"
		}
	}
	return nil
}

func stageStatic(req *Request, m *Manifest) error {
	src := filepath.Join(req.Root, filepath.FromSlash(req.Plan.Output))
	if !isDir(src) {
		return fmt.Errorf(
			"the build did not produce %s\n"+
				"       check the build output above, or set \"outputDirectory\" in %s",
			req.Plan.Output, "sorahost.json")
	}
	if err := copyTree(src, filepath.Join(req.StageIn, "public"), nil); err != nil {
		return err
	}
	m.Dir = "public"
	m.SPA = req.Plan.SPA
	return nil
}

// stageNode assembles a runnable Node application.
//
// Two shapes exist. Frameworks with a self-contained build (Next.js standalone,
// Nuxt's .output) already bundle the dependencies they need, and are copied
// verbatim. Everything else is copied source-and-all, with a production-only
// dependency install run against the copy - locally, so the server still never
// installs anything.
func stageNode(ctx context.Context, req *Request, m *Manifest) error {
	app := filepath.Join(req.StageIn, "app")
	m.Dir = "app"
	m.Start = req.Plan.Start

	if sc := req.Plan.SelfContained; sc != "" && isDir(filepath.Join(req.Root, filepath.FromSlash(sc))) {
		return stageSelfContained(req, m, app, sc)
	}

	req.Log("packaging project sources")
	ignore, err := loadIgnore(req.Root)
	if err != nil {
		return err
	}
	if err := copyTree(req.Root, app, ignore); err != nil {
		return err
	}
	if len(ignore.Secrets) > 0 && req.Warn != nil {
		req.Warn("left %d secret file(s) out of the artifact (%s) - set them as server\n  environment variables instead",
			len(ignore.Secrets), strings.Join(ignore.Secrets, ", "))
	}

	if !fileExists(filepath.Join(app, "package.json")) {
		return fmt.Errorf("this project has no package.json, so its dependencies cannot be resolved")
	}

	req.Log("installing production dependencies into the artifact")
	runner := &Runner{Dir: app, Env: req.Runner.Env, Output: req.Runner.Output}
	if err := runner.Run(ctx, req.Plan.PackageManager.ProductionInstallCommand()); err != nil {
		return fmt.Errorf("%w\n\n       Nothing is installed on the server, so runtime dependencies have to\n"+
			"       be resolved here. If your project needs a different command, set\n"+
			"       \"startCommand\" and commit a self-contained build instead", err)
	}
	return nil
}

// stageSelfContained copies a framework build that already carries its own
// dependencies, plus the sibling directories those builds expect at runtime.
func stageSelfContained(req *Request, m *Manifest, app, selfContained string) error {
	src := filepath.Join(req.Root, filepath.FromSlash(selfContained))
	req.Log("packaging self-contained build (%s)", selfContained)

	switch req.Plan.Framework {
	case "next":
		// Next.js emits the server and its trimmed node_modules into
		// .next/standalone, but deliberately leaves the static assets out of
		// it; they have to be placed back where the server expects them.
		if err := copyTree(src, app, nil); err != nil {
			return err
		}
		if static := filepath.Join(req.Root, ".next", "static"); isDir(static) {
			if err := copyTree(static, filepath.Join(app, ".next", "static"), nil); err != nil {
				return err
			}
		}
		if public := filepath.Join(req.Root, "public"); isDir(public) {
			if err := copyTree(public, filepath.Join(app, "public"), nil); err != nil {
				return err
			}
		}
		m.Start = "node server.js"
		// A monorepo standalone build nests the app under its workspace path.
		if !fileExists(filepath.Join(app, "server.js")) {
			if nested := findServerJS(app); nested != "" {
				m.Dir = filepath.ToSlash(filepath.Join("app", nested))
			}
		}
	case "nuxt":
		if err := copyTree(src, filepath.Join(app, ".output"), nil); err != nil {
			return err
		}
		m.Start = "node .output/server/index.mjs"
	default:
		if err := copyTree(src, app, nil); err != nil {
			return err
		}
	}
	return nil
}

// findServerJS locates a Next.js standalone entry nested under a workspace
// directory, returning its path relative to `root`.
func findServerJS(root string) string {
	var found string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || found != "" {
			return nil //nolint:nilerr // a partial tree is not fatal here
		}
		if !d.IsDir() && d.Name() == "server.js" {
			rel, rerr := filepath.Rel(root, filepath.Dir(path))
			if rerr == nil {
				found = rel
			}
		}
		return nil
	})
	return found
}

func measure(root string) (int, int64, error) {
	var files int
	var total int64
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		files++
		total += info.Size()
		return nil
	})
	return files, total, err
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// copyTree copies `src` into `dst`, skipping anything `skip` rejects.
//
// Symlinks are resolved to their contents when they point inside the source
// tree, and skipped otherwise. The artifact format contains no links at all, so
// resolving here is what makes `node_modules/.bin` and pnpm's store layout
// survive the trip.
func copyTree(src, dst string, skip *ignoreList) error {
	srcAbs, err := filepath.Abs(src)
	if err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dst, 0o755)
		}
		slashRel := filepath.ToSlash(rel)
		if skip != nil && skip.matches(slashRel, d.IsDir()) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		target := filepath.Join(dst, rel)
		if d.Type()&os.ModeSymlink != 0 {
			return copySymlink(srcAbs, path, target)
		}
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		if !d.Type().IsRegular() {
			// Sockets, devices and FIFOs cannot be represented in an artifact.
			return nil
		}
		return copyFile(path, target)
	})
}

// copySymlink materialises a link's target, but only when it stays inside the
// tree being copied - a link out to the wider filesystem would silently pull
// files the developer never meant to publish.
func copySymlink(srcRoot, link, target string) error {
	resolved, err := filepath.EvalSymlinks(link)
	if err != nil {
		return nil // dangling link: nothing to copy
	}
	if !strings.HasPrefix(resolved, srcRoot+string(os.PathSeparator)) && resolved != srcRoot {
		return nil
	}
	st, err := os.Stat(resolved)
	if err != nil {
		return nil
	}
	if st.IsDir() {
		return copyTree(resolved, target, nil)
	}
	return copyFile(resolved, target)
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	info, err := in.Stat()
	if err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info.Mode()&0o111 != 0 {
		mode = 0o755
	}
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

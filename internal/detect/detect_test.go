package detect

import (
	"os"
	"path/filepath"
	"testing"
)

// project writes a throwaway project tree and returns its root.
func project(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestDetect(t *testing.T) {
	cases := []struct {
		name      string
		files     map[string]string
		framework string
		mode      Mode
		check     func(t *testing.T, p *Plan)
	}{
		{
			name: "next defaults to a node deployment",
			files: map[string]string{
				"package.json": `{"dependencies":{"next":"15.0.0"}}`,
			},
			framework: "next",
			mode:      ModeNode,
			check: func(t *testing.T, p *Plan) {
				if p.SelfContained != ".next/standalone" {
					t.Errorf("SelfContained = %q, want .next/standalone", p.SelfContained)
				}
			},
		},
		{
			name: "next with output export is static",
			files: map[string]string{
				"package.json":   `{"dependencies":{"next":"15.0.0"}}`,
				"next.config.js": `module.exports = { output: "export" }`,
			},
			framework: "next",
			mode:      ModeStatic,
			check: func(t *testing.T, p *Plan) {
				if p.Output != "out" {
					t.Errorf("Output = %q, want out", p.Output)
				}
			},
		},
		{
			name: "sveltekit with adapter-node runs as node",
			files: map[string]string{
				"package.json": `{"devDependencies":{"@sveltejs/kit":"2","@sveltejs/adapter-node":"5"}}`,
			},
			framework: "sveltekit",
			mode:      ModeNode,
		},
		{
			name: "sveltekit without an adapter is static",
			files: map[string]string{
				"package.json": `{"devDependencies":{"@sveltejs/kit":"2"}}`,
			},
			framework: "sveltekit",
			mode:      ModeStatic,
		},
		{
			name: "a vite app is a static SPA",
			files: map[string]string{
				"package.json": `{"devDependencies":{"vite":"5","react":"18"}}`,
			},
			framework: "vite",
			mode:      ModeStatic,
			check: func(t *testing.T, p *Plan) {
				if !p.SPA {
					t.Error("SPA = false, want true")
				}
			},
		},
		{
			name: "a workers module is detected from its default export",
			files: map[string]string{
				"package.json":  `{}`,
				"src/index.js":  "export default { async fetch(req) { return new Response('hi') } }",
				"wrangler.toml": `main = "src/index.js"`,
			},
			framework: "workers",
			mode:      ModeWorker,
			check: func(t *testing.T, p *Plan) {
				if p.Entry != "src/index.js" {
					t.Errorf("Entry = %q, want src/index.js", p.Entry)
				}
			},
		},
		{
			name: "hono is recognised as a worker",
			files: map[string]string{
				"package.json": `{"dependencies":{"hono":"4"}}`,
				"src/index.ts": "import { Hono } from 'hono'\nconst app = new Hono()\nexport default { fetch: app.fetch }",
			},
			framework: "hono",
			mode:      ModeWorker,
		},
		{
			name: "a plain html directory is static",
			files: map[string]string{
				"dist/index.html": "<h1>hi</h1>",
			},
			framework: "static",
			mode:      ModeStatic,
			check: func(t *testing.T, p *Plan) {
				if p.Install != "" {
					t.Errorf("Install = %q, want no install step", p.Install)
				}
			},
		},
		{
			name: "an express app runs as node",
			files: map[string]string{
				"package.json": `{"main":"server.js","dependencies":{"express":"4"}}`,
			},
			framework: "express",
			mode:      ModeNode,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plan, err := Detect(project(t, tc.files))
			if err != nil {
				t.Fatalf("Detect: %v", err)
			}
			if plan.Framework != tc.framework {
				t.Errorf("Framework = %q, want %q", plan.Framework, tc.framework)
			}
			if plan.Mode != tc.mode {
				t.Errorf("Mode = %q, want %q", plan.Mode, tc.mode)
			}
			if tc.check != nil {
				tc.check(t, plan)
			}
		})
	}
}

func TestDetectUnrecognisedProject(t *testing.T) {
	root := project(t, map[string]string{"notes.txt": "nothing to build here"})
	if _, err := Detect(root); err == nil {
		t.Fatal("Detect succeeded on a project it cannot build")
	}
}

func TestProjectScriptsWinOverFrameworkDefaults(t *testing.T) {
	root := project(t, map[string]string{
		"package.json": `{"scripts":{"build":"vite build --mode prod"},"devDependencies":{"vite":"5"}}`,
	})
	plan, err := Detect(root)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Build != "npm run build" {
		t.Errorf("Build = %q, want the project's own script", plan.Build)
	}
}

func TestDetectPackageManagerPrefersLockfile(t *testing.T) {
	root := project(t, map[string]string{
		"package.json":   `{}`,
		"pnpm-lock.yaml": "lockfileVersion: '9.0'",
	})
	// The assertion only holds where pnpm is installed; elsewhere falling back
	// to npm is the documented behaviour, so both outcomes are correct.
	got := DetectPackageManager(root, readPackageJSON(root))
	if Available(PNPM) && got != PNPM {
		t.Errorf("PackageManager = %q, want pnpm", got)
	}
	if !Available(PNPM) && got != NPM {
		t.Errorf("PackageManager = %q, want npm fallback", got)
	}
}

func TestInstallCommandUsesFrozenVariantWithLockfile(t *testing.T) {
	withLock := project(t, map[string]string{"package.json": `{}`, "package-lock.json": `{}`})
	if got := NPM.InstallCommand(withLock); got != "npm ci --no-audit --no-fund" {
		t.Errorf("InstallCommand = %q, want npm ci", got)
	}
	withoutLock := project(t, map[string]string{"package.json": `{}`})
	if got := NPM.InstallCommand(withoutLock); got != "npm install --no-audit --no-fund" {
		t.Errorf("InstallCommand = %q, want npm install", got)
	}
}

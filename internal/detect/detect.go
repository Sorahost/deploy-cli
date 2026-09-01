// Package detect works out how a project should be installed, built and served.
//
// Detection is a convenience, never a requirement: every field it produces can
// be overridden in sorahost.json, and an unrecognised project falls back to the
// scripts the author already wrote. When a guess would be a coin flip, detect
// says so rather than picking silently.
package detect

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Mode is how a deployment is served.
type Mode string

const (
	// ModeWorker serves a Workers-style module (export default { fetch }).
	ModeWorker Mode = "worker"
	// ModeStatic serves a directory of files built ahead of time.
	ModeStatic Mode = "static"
	// ModeNode runs a Node server behind the runtime.
	ModeNode Mode = "node"
)

// Plan is everything the CLI needs to build and package a project.
type Plan struct {
	Framework string
	Mode      Mode

	Install string // command to install dependencies locally
	Build   string // command to produce the build output ("" when none)
	Start   string // node mode: command the server runs

	Output string // static mode: directory the build emits
	Entry  string // worker mode: module entry, relative to the project root
	SPA    bool   // static mode: serve index.html for unknown paths

	// PackageManager is the manager the project declares, so the build uses the
	// same one the author tested with.
	PackageManager PackageManager

	// SelfContained marks a node build whose output already includes every
	// dependency it needs (Next.js standalone, Nuxt's .output). Those do not
	// need node_modules copied into the artifact.
	SelfContained string // directory, relative to the project root
}

// Detect inspects `root` and returns a plan.
func Detect(root string) (*Plan, error) {
	pkg := readPackageJSON(root)
	pm := DetectPackageManager(root, pkg)

	plan := &Plan{Framework: "unknown", PackageManager: pm}
	plan.Install = pm.InstallCommand(root)

	if entry := findWorkerEntry(root, pkg); entry != "" {
		plan.Mode = ModeWorker
		plan.Entry = entry
		plan.Framework = "workers"
		if pkg.has("hono") {
			plan.Framework = "hono"
		}
		// A Workers project is bundled by the CLI, so its own build script (if
		// any) is only run when it produces assets we would otherwise miss.
		if pkg.script("build") != "" && dirExists(filepath.Join(root, "public")) {
			plan.Build = pm.RunCommand("build")
			plan.Output = "public"
		}
		return plan, nil
	}

	if f := matchFramework(root, pkg); f != nil {
		plan.Framework = f.id
		f.apply(root, pkg, plan)
	} else if err := fallback(root, pkg, plan); err != nil {
		return nil, err
	}

	// The project's own scripts are what its author actually tested, so they
	// win over the framework's canonical command.
	if pkg.script("build") != "" && plan.Mode != ModeWorker {
		plan.Build = pm.RunCommand("build")
	}
	if plan.Mode == ModeNode && pkg.script("start") != "" && plan.Start == "" {
		plan.Start = pm.RunCommand("start")
	}
	return plan, nil
}

// framework is one entry in the detection table.
type framework struct {
	id    string
	dep   string
	apply func(root string, pkg *packageJSON, p *Plan)
}

// Frameworks in priority order; the first whose dependency is present wins.
// Order matters where one framework is built on another - Nuxt depends on Vite,
// so Nuxt has to be considered first.
var frameworks = []framework{
	{"next", "next", func(root string, pkg *packageJSON, p *Plan) {
		p.Mode = ModeNode
		p.Build = "next build"
		p.Start = "node server.js"
		// `output: "standalone"` emits a server bundle with its own trimmed
		// node_modules. Whether it is enabled is only knowable after the build,
		// so the packer re-checks; this records where to look.
		p.SelfContained = ".next/standalone"
		if isNextStaticExport(root) {
			p.Mode = ModeStatic
			p.Output = "out"
			p.Start = ""
			p.SelfContained = ""
		}
	}},
	{"nuxt", "nuxt", func(_ string, _ *packageJSON, p *Plan) {
		p.Mode = ModeNode
		p.Build = "nuxt build"
		p.Start = "node .output/server/index.mjs"
		p.SelfContained = ".output"
	}},
	{"sveltekit", "@sveltejs/kit", func(_ string, pkg *packageJSON, p *Plan) {
		p.Build = "vite build"
		if pkg.has("@sveltejs/adapter-node") {
			p.Mode = ModeNode
			p.Start = "node build/index.js"
			return
		}
		p.Mode = ModeStatic
		p.Output = "build"
	}},
	{"astro", "astro", func(_ string, pkg *packageJSON, p *Plan) {
		p.Build = "astro build"
		if pkg.has("@astrojs/node") {
			p.Mode = ModeNode
			p.Start = "node ./dist/server/entry.mjs"
			return
		}
		p.Mode = ModeStatic
		p.Output = "dist"
	}},
	{"remix", "@remix-run/node", func(_ string, _ *packageJSON, p *Plan) {
		p.Mode = ModeNode
		p.Build = "remix vite:build"
		p.Start = "remix-serve ./build/server/index.js"
	}},
	{"nest", "@nestjs/core", func(_ string, _ *packageJSON, p *Plan) {
		p.Mode = ModeNode
		p.Build = "nest build"
		p.Start = "node dist/main.js"
	}},
	{"angular", "@angular/core", func(root string, _ *packageJSON, p *Plan) {
		p.Mode = ModeStatic
		p.Build = "ng build"
		p.Output = angularOutput(root)
		p.SPA = true
	}},
	{"vite", "vite", func(_ string, pkg *packageJSON, p *Plan) {
		p.Mode = ModeStatic
		p.Build = "vite build"
		p.Output = "dist"
		// Vue and React SPAs both route in the browser; a static site generator
		// on top of Vite would have set a more specific framework above.
		p.SPA = true
	}},
	{"vue-cli", "@vue/cli-service", func(_ string, _ *packageJSON, p *Plan) {
		p.Mode = ModeStatic
		p.Build = "vue-cli-service build"
		p.Output = "dist"
		p.SPA = true
	}},
	{"cra", "react-scripts", func(_ string, _ *packageJSON, p *Plan) {
		p.Mode = ModeStatic
		p.Build = "react-scripts build"
		p.Output = "build"
		p.SPA = true
	}},
	{"eleventy", "@11ty/eleventy", func(_ string, _ *packageJSON, p *Plan) {
		p.Mode = ModeStatic
		p.Build = "eleventy"
		p.Output = "_site"
	}},
	{"express", "express", func(_ string, _ *packageJSON, p *Plan) { p.Mode = ModeNode }},
	{"fastify", "fastify", func(_ string, _ *packageJSON, p *Plan) { p.Mode = ModeNode }},
	{"koa", "koa", func(_ string, _ *packageJSON, p *Plan) { p.Mode = ModeNode }},
	{"hapi", "@hapi/hapi", func(_ string, _ *packageJSON, p *Plan) { p.Mode = ModeNode }},
}

func matchFramework(root string, pkg *packageJSON) *framework {
	for i := range frameworks {
		if pkg.has(frameworks[i].dep) {
			return &frameworks[i]
		}
	}
	_ = root
	return nil
}

// fallback handles projects with no recognisable framework dependency, using
// what the repository looks like on disk.
func fallback(root string, pkg *packageJSON, p *Plan) error {
	if pkg != nil && (pkg.script("start") != "" || pkg.Main != "") {
		p.Mode = ModeNode
		if p.Start == "" && pkg.Main != "" {
			p.Start = "node " + pkg.Main
		}
		if p.Start == "" {
			p.Start = "node index.js"
		}
		return nil
	}
	for _, dir := range []string{"dist", "build", "out", "public", "_site", "."} {
		if fileExists(filepath.Join(root, dir, "index.html")) {
			p.Mode = ModeStatic
			p.Output = dir
			p.Framework = "static"
			p.SPA = fileExists(filepath.Join(root, dir, "200.html"))
			if pkg == nil {
				p.Install = ""
			}
			return nil
		}
	}
	return fmt.Errorf(
		"could not work out how to build this project\n" +
			"       add a sorahost.json with \"mode\", \"buildCommand\" and \"outputDirectory\",\n" +
			"       or run `sorahost link` again from the project root",
	)
}

// --- package.json -----------------------------------------------------------

type packageJSON struct {
	Main            string            `json:"main"`
	Scripts         map[string]string `json:"scripts"`
	Dependencies    map[string]string `json:"dependencies"`
	DevDependencies map[string]string `json:"devDependencies"`
	PackageManager  string            `json:"packageManager"`
}

func (p *packageJSON) has(dep string) bool {
	if p == nil {
		return false
	}
	_, a := p.Dependencies[dep]
	_, b := p.DevDependencies[dep]
	return a || b
}

func readPackageJSON(root string) *packageJSON {
	raw, err := os.ReadFile(filepath.Join(root, "package.json"))
	if err != nil {
		return nil
	}
	var p packageJSON
	if json.Unmarshal(raw, &p) != nil {
		return nil
	}
	if p.Scripts == nil {
		p.Scripts = map[string]string{}
	}
	return &p
}

// --- worker entry -----------------------------------------------------------

var workerEntries = []string{
	"src/index.ts", "src/index.js", "src/index.mjs",
	"src/worker.ts", "src/worker.js",
	"index.ts", "index.js", "worker.js", "worker.ts",
}

var (
	wranglerMain  = regexp.MustCompile(`["']?main["']?\s*[:=]\s*["']([^"']+)["']`)
	defaultExport = regexp.MustCompile(`(?m)export\s+default\s*\{|export\s+default\s+[A-Za-z_$][\w$]*\s*;?\s*$`)
	fetchHandler  = regexp.MustCompile(`fetch\s*[(:]`)
)

// findWorkerEntry looks for a module that exports a default object with a fetch
// handler - the shape Cloudflare Workers and Hono both use.
func findWorkerEntry(root string, pkg *packageJSON) string {
	var candidates []string
	for _, name := range []string{"wrangler.toml", "wrangler.json", "wrangler.jsonc"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		if m := wranglerMain.FindSubmatch(raw); m != nil {
			candidates = append(candidates, string(m[1]))
		}
		break
	}
	if pkg != nil && pkg.Main != "" {
		candidates = append(candidates, pkg.Main)
	}
	candidates = append(candidates, workerEntries...)

	for _, rel := range candidates {
		rel = filepath.ToSlash(filepath.Clean(rel))
		if strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil {
			continue
		}
		if defaultExport.Match(src) && fetchHandler.Match(src) {
			return rel
		}
	}
	return ""
}

// --- small helpers ----------------------------------------------------------

// isNextStaticExport reports whether next.config.* asks for a static export,
// which turns a Next.js project into a plain directory of files.
func isNextStaticExport(root string) bool {
	for _, name := range []string{"next.config.js", "next.config.mjs", "next.config.ts", "next.config.cjs"} {
		raw, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			continue
		}
		if regexp.MustCompile(`output\s*:\s*["']export["']`).Match(raw) {
			return true
		}
	}
	return false
}

// angularOutput reads the configured output path, which Angular projects
// customise far more often than other frameworks do.
func angularOutput(root string) string {
	raw, err := os.ReadFile(filepath.Join(root, "angular.json"))
	if err != nil {
		return "dist"
	}
	if m := regexp.MustCompile(`"outputPath"\s*:\s*"([^"]+)"`).FindSubmatch(raw); m != nil {
		return filepath.ToSlash(filepath.Clean(string(m[1])))
	}
	return "dist"
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// script returns a package script, tolerating a project with no package.json.
func (p *packageJSON) script(name string) string {
	if p == nil {
		return ""
	}
	return p.Scripts[name]
}

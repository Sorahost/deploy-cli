package detect

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// PackageManager is the tool used to install dependencies and run scripts.
//
// Which one is used is not cosmetic: frameworks such as Next.js read the
// lockfile themselves and shell out to that manager mid-build, so running the
// wrong one produces confusing failures deep inside someone else's tool.
type PackageManager string

const (
	NPM  PackageManager = "npm"
	PNPM PackageManager = "pnpm"
	Yarn PackageManager = "yarn"
	Bun  PackageManager = "bun"
)

type pmSpec struct {
	lockfile string
	install  string
	frozen   string
	run      string
}

var pmSpecs = map[PackageManager]pmSpec{
	NPM:  {"package-lock.json", "npm install --no-audit --no-fund", "npm ci --no-audit --no-fund", "npm run"},
	PNPM: {"pnpm-lock.yaml", "pnpm install", "pnpm install --frozen-lockfile", "pnpm run"},
	Yarn: {"yarn.lock", "yarn install", "yarn install --immutable", "yarn run"},
	Bun:  {"bun.lockb", "bun install", "bun install --frozen-lockfile", "bun run"},
}

// DetectPackageManager picks the manager the project declares, preferring an
// explicit `packageManager` field over the lockfile that happens to be present.
func DetectPackageManager(root string, pkg *packageJSON) PackageManager {
	if pkg != nil && pkg.PackageManager != "" {
		name := PackageManager(strings.SplitN(pkg.PackageManager, "@", 2)[0])
		if _, ok := pmSpecs[name]; ok && Available(name) {
			return name
		}
	}
	for _, pm := range []PackageManager{PNPM, Yarn, Bun} {
		if fileExists(filepath.Join(root, pmSpecs[pm].lockfile)) && Available(pm) {
			return pm
		}
	}
	// bun writes a text lockfile in newer versions.
	if fileExists(filepath.Join(root, "bun.lock")) && Available(Bun) {
		return Bun
	}
	return NPM
}

// Available reports whether the manager can actually be executed here. A
// lockfile can name a tool the developer has not installed, and falling back to
// npm with a clear warning beats failing with "executable file not found".
func Available(pm PackageManager) bool {
	_, err := exec.LookPath(string(pm))
	return err == nil
}

// InstallCommand is the install command for this project, using the frozen
// variant when a lockfile is present so local builds match CI exactly.
func (pm PackageManager) InstallCommand(root string) string {
	spec := pmSpecs[pm]
	if fileExists(filepath.Join(root, spec.lockfile)) || (pm == Bun && fileExists(filepath.Join(root, "bun.lock"))) {
		return spec.frozen
	}
	return spec.install
}

// ProductionInstallCommand installs only runtime dependencies, for the copy of
// a node project that ships inside the artifact.
func (pm PackageManager) ProductionInstallCommand() string {
	switch pm {
	case PNPM:
		return "pnpm install --prod --ignore-scripts=false"
	case Yarn:
		return "yarn workspaces focus --production"
	case Bun:
		return "bun install --production"
	default:
		return "npm install --omit=dev --no-audit --no-fund"
	}
}

// RunCommand runs one of the project's package scripts.
func (pm PackageManager) RunCommand(script string) string {
	return pmSpecs[pm].run + " " + script
}

// Lockfile is the lockfile this manager writes.
func (pm PackageManager) Lockfile() string { return pmSpecs[pm].lockfile }

// Env is extra environment this manager needs.
//
// pnpm 10 refuses to run dependency build scripts unless told to trust them,
// and turns that refusal into a non-zero exit. A deploy tool installs what the
// repository asks for - packages like sharp are useless without their
// postinstall - so the check is disabled rather than left to fail confusingly.
func (pm PackageManager) Env() []string {
	if pm != PNPM {
		return nil
	}
	return []string{
		"npm_config_dangerously_allow_all_builds=true",
		"npm_config_strict_dep_builds=false",
	}
}

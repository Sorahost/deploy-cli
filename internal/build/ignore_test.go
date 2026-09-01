package build

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultIgnoresExcludeSecretsAndJunk(t *testing.T) {
	l, err := loadIgnore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	excluded := []string{
		".env",
		".env.local",
		".env.production",
		"config/.env",
		"certs/server.key",
		"certs/server.pem",
		".npmrc",
		".netrc",
		"node_modules/react/index.js",
		".git/config",
		"deploy.log",
	}
	for _, path := range excluded {
		if !l.matches(path, false) {
			t.Errorf("%s was packed, but must never leave the developer's machine", path)
		}
	}

	included := []string{
		"src/index.ts",
		"package.json",
		".env.example",
		"public/env.png",
		"src/keyboard.ts",
	}
	for _, path := range included {
		if l.matches(path, false) {
			t.Errorf("%s was excluded, but the project needs it", path)
		}
	}
}

func TestIgnoreDirectoriesMatchAtAnyDepth(t *testing.T) {
	l, err := loadIgnore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if !l.matches("packages/web/node_modules", true) {
		t.Error("a nested node_modules was not excluded")
	}
}

func TestSorahostIgnoreAddsPatterns(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, IgnoreFile),
		[]byte("# comment\n\nfixtures/\n*.mp4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	l, err := loadIgnore(root)
	if err != nil {
		t.Fatal(err)
	}
	if !l.matches("fixtures", true) || !l.matches("fixtures/big.json", false) {
		t.Error("an anchored directory pattern from .sorahostignore was not applied")
	}
	if !l.matches("media/intro.mp4", false) {
		t.Error("a glob pattern from .sorahostignore was not applied")
	}
	if l.matches("src/app.ts", false) {
		t.Error("an unrelated file was excluded")
	}
}

// A secret that is skipped has to be reported, because skipping it silently
// changes how the deployment behaves.
func TestSkippedSecretsAreRecorded(t *testing.T) {
	l, err := loadIgnore(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	l.matches(".env", false)
	l.matches("src/index.ts", false)

	if len(l.Secrets) != 1 || l.Secrets[0] != ".env" {
		t.Errorf("Secrets = %v, want [.env]", l.Secrets)
	}
}

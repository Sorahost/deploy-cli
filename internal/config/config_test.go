package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestValidateEndpoint(t *testing.T) {
	valid := []string{
		"https://app.example.com/_sorahost/Q8y10DRDZffm",
		"http://127.0.0.1:8080/_sorahost/abc",
	}
	for _, raw := range valid {
		if err := ValidateEndpoint(raw); err != nil {
			t.Errorf("ValidateEndpoint(%q) = %v, want nil", raw, err)
		}
	}

	invalid := map[string]string{
		"no scheme":      "app.example.com/_sorahost/abc",
		"wrong scheme":   "ftp://app.example.com/_sorahost/abc",
		"no api path":    "https://app.example.com",
		"root path":      "https://app.example.com/",
		"has a query":    "https://app.example.com/_sorahost/abc?x=1",
		"has a fragment": "https://app.example.com/_sorahost/abc#f",
	}
	for name, raw := range invalid {
		if err := ValidateEndpoint(raw); err == nil {
			t.Errorf("ValidateEndpoint(%s: %q) = nil, want an error", name, raw)
		}
	}
}

// Two spellings of the same endpoint must resolve to one stored token, or a
// trailing slash silently produces a second, stale credential.
func TestCredentialsNormaliseEndpoints(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("SORAHOST_CONFIG_DIR", dir)
	t.Setenv(EnvToken, "")

	creds, err := LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := creds.Set("https://APP.example.com/_sorahost/abc/", "shd_secret"); err != nil {
		t.Fatal(err)
	}

	reloaded, err := LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	token, ok := reloaded.Token("https://app.example.com/_sorahost/abc")
	if !ok || token != "shd_secret" {
		t.Fatalf("Token = %q, %v; want the stored token", token, ok)
	}
}

func TestCredentialsEnvironmentWins(t *testing.T) {
	t.Setenv("SORAHOST_CONFIG_DIR", t.TempDir())
	t.Setenv(EnvToken, "shd_from_ci")

	creds, err := LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := creds.Set("https://app.example.com/_sorahost/abc", "shd_stored"); err != nil {
		t.Fatal(err)
	}
	if token, _ := creds.Token("https://app.example.com/_sorahost/abc"); token != "shd_from_ci" {
		t.Errorf("Token = %q, want the environment's token", token)
	}
}

// The credential file holds a secret; it must not be world-readable.
func TestCredentialsFileIsPrivate(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX modes are advisory on Windows")
	}
	dir := t.TempDir()
	t.Setenv("SORAHOST_CONFIG_DIR", dir)
	t.Setenv(EnvToken, "")

	creds, err := LoadCredentials()
	if err != nil {
		t.Fatal(err)
	}
	if err := creds.Set("https://app.example.com/_sorahost/abc", "shd_secret"); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(creds.Path())
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Errorf("credentials mode = %04o, want no group or world access", perm)
	}
}

func TestProjectRejectsInvalidNames(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ProjectFile)
	if err := os.WriteFile(path, []byte(`{
		"endpoint": "https://app.example.com/_sorahost/abc",
		"vars": { "lower_case": "no" }
	}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadProject(path); err == nil {
		t.Fatal("LoadProject accepted a lower-case variable name")
	}
}

func TestFindProjectWalksUp(t *testing.T) {
	root := t.TempDir()
	nested := filepath.Join(root, "src", "components")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, ProjectFile),
		[]byte(`{"endpoint":"https://app.example.com/_sorahost/abc"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	p, err := FindProject(nested)
	if err != nil {
		t.Fatalf("FindProject: %v", err)
	}
	if got, want := filepath.Clean(p.Dir()), filepath.Clean(root); got != want {
		t.Errorf("Dir = %q, want %q", got, want)
	}
}

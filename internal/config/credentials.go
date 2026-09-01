package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// Credentials stores one deploy token per endpoint.
//
// It lives outside the project on purpose. A token in the repository is a token
// in the next `git push`, so `sorahost.json` holds only the endpoint and the
// secret is kept in the user's config directory with 0600 permissions.
type Credentials struct {
	Version int               `json:"version"`
	Tokens  map[string]string `json:"tokens"` // endpoint -> token
	path    string            `json:"-"`
}

const credentialsVersion = 1

// EnvEndpoint and EnvToken let CI supply credentials without a config file.
const (
	EnvEndpoint = "SORAHOST_ENDPOINT"
	EnvToken    = "SORAHOST_TOKEN"
)

// CredentialsPath is where the store lives on this platform.
// It follows each platform's own convention rather than scattering dotfiles.
func CredentialsPath() (string, error) {
	if custom := os.Getenv("SORAHOST_CONFIG_DIR"); custom != "" {
		return filepath.Join(custom, "credentials.json"), nil
	}
	var base string
	switch runtime.GOOS {
	case "windows":
		base = os.Getenv("AppData")
		if base == "" {
			return "", errors.New("cannot locate %AppData%; set SORAHOST_CONFIG_DIR")
		}
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		base = filepath.Join(home, "Library", "Application Support")
	default:
		if base = os.Getenv("XDG_CONFIG_HOME"); base == "" {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", err
			}
			base = filepath.Join(home, ".config")
		}
	}
	return filepath.Join(base, "sorahost", "credentials.json"), nil
}

// LoadCredentials reads the store, returning an empty one if it does not exist.
func LoadCredentials() (*Credentials, error) {
	path, err := CredentialsPath()
	if err != nil {
		return nil, err
	}
	c := &Credentials{Version: credentialsVersion, Tokens: map[string]string{}, path: path}
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return c, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(raw, c); err != nil {
		return nil, fmt.Errorf("%s is corrupt: %w", path, err)
	}
	if c.Tokens == nil {
		c.Tokens = map[string]string{}
	}
	c.path = path
	return c, nil
}

// Token returns the token to use for `endpoint`.
//
// The environment wins, so a CI job can override a developer's stored token
// without touching their machine's config.
func (c *Credentials) Token(endpoint string) (string, bool) {
	if t := strings.TrimSpace(os.Getenv(EnvToken)); t != "" {
		return t, true
	}
	t, ok := c.Tokens[normalizeEndpoint(endpoint)]
	return t, ok && t != ""
}

// Set stores a token for an endpoint and persists the store.
func (c *Credentials) Set(endpoint, token string) error {
	c.Version = credentialsVersion
	c.Tokens[normalizeEndpoint(endpoint)] = token
	return c.save()
}

// Forget removes an endpoint's token. It is not an error if none was stored.
func (c *Credentials) Forget(endpoint string) error {
	delete(c.Tokens, normalizeEndpoint(endpoint))
	return c.save()
}

// Endpoints lists the endpoints with a stored token.
func (c *Credentials) Endpoints() []string {
	out := make([]string, 0, len(c.Tokens))
	for k := range c.Tokens {
		out = append(out, k)
	}
	return out
}

// Path is the file the store reads from and writes to.
func (c *Credentials) Path() string { return c.path }

func (c *Credentials) save() error {
	dir := filepath.Dir(c.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	// Write to a sibling and rename, so an interrupted save cannot leave a
	// half-written file where a token used to be.
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, c.path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// On Windows the mode bits above are advisory; the file still inherits the
	// user-private ACL of %AppData%.
	_ = os.Chmod(c.path, 0o600)
	return nil
}

// ValidateEndpoint checks that an endpoint is a URL we are willing to send a
// token to.
func ValidateEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("endpoint is not a URL: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("endpoint must be http or https (got %q)", u.Scheme)
	}
	if u.Host == "" {
		return errors.New("endpoint has no host")
	}
	if u.Path == "" || u.Path == "/" {
		return errors.New("endpoint is missing its API path - copy the full URL from the server console")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return errors.New("endpoint must not contain a query string or fragment")
	}
	return nil
}

// normalizeEndpoint makes lookups insensitive to trailing slashes and host case,
// so two spellings of the same server do not end up with two stored tokens.
func normalizeEndpoint(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return strings.TrimRight(strings.TrimSpace(raw), "/")
	}
	u.Host = strings.ToLower(u.Host)
	u.Path = strings.TrimRight(u.Path, "/")
	return u.String()
}

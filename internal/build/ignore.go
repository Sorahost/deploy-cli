package build

import (
	"bufio"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// IgnoreFile lets a project exclude extra paths from its artifact.
const IgnoreFile = ".sorahostignore"

// defaultIgnores are excluded from every artifact.
//
// The first group is dead weight - source control, caches, and the local
// node_modules that a production install replaces. The second group is the
// point of the list: files that hold secrets. An artifact is uploaded to a
// server and unpacked into a directory the deployment can read, so a `.env`
// that ships with it is a `.env` that has left the developer's machine.
var defaultIgnores = []string{
	".git", ".hg", ".svn",
	"node_modules",
	".sorahost", ".sorahostignore",
	".next/cache", ".nuxt", ".svelte-kit", ".turbo", ".parcel-cache", ".cache",
	".vercel", ".netlify", ".wrangler",
	".DS_Store", "Thumbs.db",
	"*.log",

	".env", ".env.*",
	"*.pem", "*.key", "*.p12", "*.pfx",
	".npmrc", ".yarnrc.yml", ".netrc",
	"id_rsa", "id_ed25519",
}

// keepEnv are the .env variants that are conventionally committed and hold
// defaults rather than secrets; excluding them breaks builds for no benefit.
var keepEnv = map[string]bool{
	".env.example":  true,
	".env.sample":   true,
	".env.template": true,
	".env.defaults": true,
}

type ignoreList struct {
	patterns []string
	// Secrets records which secret-bearing files were skipped, so the CLI can
	// tell the user rather than silently changing their deployment's behaviour.
	Secrets []string
}

// loadIgnore builds the exclusion list for a project.
func loadIgnore(root string) (*ignoreList, error) {
	l := &ignoreList{patterns: append([]string(nil), defaultIgnores...)}

	f, err := os.Open(filepath.Join(root, IgnoreFile))
	if errors.Is(err, os.ErrNotExist) {
		return l, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		l.patterns = append(l.patterns, strings.TrimSuffix(filepath.ToSlash(line), "/"))
	}
	return l, scanner.Err()
}

// matches reports whether a path relative to the project root is excluded.
// `rel` uses forward slashes on every platform.
func (l *ignoreList) matches(rel string, isDir bool) bool {
	segments := strings.Split(rel, "/")
	if keep, known := keepEnv[segments[len(segments)-1]]; known && keep {
		return false
	}

	for _, pattern := range l.patterns {
		// A pattern with a slash is anchored to the project root.
		if strings.Contains(pattern, "/") {
			if rel == pattern || strings.HasPrefix(rel, pattern+"/") {
				return true
			}
			if ok, _ := path.Match(pattern, rel); ok {
				return true
			}
			continue
		}
		// A bare name matches that name at any depth, and takes everything
		// below it with it - which is what people expect from `node_modules`.
		for _, segment := range segments {
			if ok, _ := path.Match(pattern, segment); ok {
				if isSecretPattern(pattern) {
					l.Secrets = append(l.Secrets, rel)
				}
				return true
			}
		}
	}
	return false
}

func isSecretPattern(pattern string) bool {
	switch {
	case strings.HasPrefix(pattern, ".env"),
		strings.HasSuffix(pattern, ".pem"), strings.HasSuffix(pattern, ".key"),
		strings.HasSuffix(pattern, ".p12"), strings.HasSuffix(pattern, ".pfx"),
		pattern == ".npmrc", pattern == ".netrc", strings.HasPrefix(pattern, "id_"):
		return true
	}
	return false
}

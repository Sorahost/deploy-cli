// Package config reads the two pieces of state the CLI keeps: the project file
// that belongs in version control, and the credential store that must never go
// anywhere near it.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ProjectFile is the name of the per-project configuration file.
const ProjectFile = "sorahost.json"

// Project is `sorahost.json` in the project root.
//
// Everything here is safe to commit: it records which server the project
// deploys to and any overrides for the detected build, but never a token.
type Project struct {
	// Endpoint is the full deploy API URL, including the random path segment.
	Endpoint string `json:"endpoint"`
	// Name is a label used in output only.
	Name string `json:"name,omitempty"`

	// Overrides for detection. An empty field means "detect it".
	Mode      string `json:"mode,omitempty"`      // worker | static | node
	Framework string `json:"framework,omitempty"` //
	Install   string `json:"installCommand,omitempty"`
	Build     string `json:"buildCommand,omitempty"`
	Start     string `json:"startCommand,omitempty"`
	Output    string `json:"outputDirectory,omitempty"`
	Entry     string `json:"entry,omitempty"` // worker module entry
	SPA       *bool  `json:"spa,omitempty"`

	// Runtime configuration handed to the deployment.
	Vars map[string]any `json:"vars,omitempty"`
	Env  []string       `json:"env,omitempty"`

	CompatibilityDate  string   `json:"compatibilityDate,omitempty"`
	CompatibilityFlags []string `json:"compatibilityFlags,omitempty"`

	path string
}

// FindProject walks up from `dir` looking for a project file, so the CLI works
// from any subdirectory the way git does.
func FindProject(dir string) (*Project, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	for {
		candidate := filepath.Join(abs, ProjectFile)
		if _, err := os.Stat(candidate); err == nil {
			return LoadProject(candidate)
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return nil, ErrNoProject
		}
		abs = parent
	}
}

// ErrNoProject is returned when no sorahost.json exists above the working
// directory.
var ErrNoProject = errors.New("this directory is not linked to a SORAHOST server")

// LoadProject reads a project file from an explicit path.
func LoadProject(path string) (*Project, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var p Project
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	if err := p.Validate(); err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	p.path = path
	return &p, nil
}

// Validate rejects a project file that would produce a broken deployment,
// while the user is still looking at their own terminal.
func (p *Project) Validate() error {
	if p.Endpoint == "" {
		return errors.New(`"endpoint" is required`)
	}
	if err := ValidateEndpoint(p.Endpoint); err != nil {
		return err
	}
	switch p.Mode {
	case "", "worker", "static", "node":
	default:
		return fmt.Errorf(`"mode" must be worker, static or node (got %q)`, p.Mode)
	}
	for name := range p.Vars {
		if !isEnvName(name) {
			return fmt.Errorf(`vars: %q is not a valid environment variable name`, name)
		}
	}
	for _, name := range p.Env {
		if !isEnvName(name) {
			return fmt.Errorf(`env: %q is not a valid environment variable name`, name)
		}
	}
	return nil
}

// Dir is the project root: the directory holding sorahost.json.
func (p *Project) Dir() string { return filepath.Dir(p.path) }

// Path is the location the project file was read from or will be written to.
func (p *Project) Path() string { return p.path }

// Save writes the project file with stable formatting so that repeated runs
// produce no incidental diffs.
func (p *Project) Save(path string) error {
	raw, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil {
		return err
	}
	p.path = path
	return nil
}

func isEnvName(s string) bool {
	if s == "" || s[0] < 'A' || s[0] > 'Z' {
		return false
	}
	for _, r := range s {
		if !(r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_') {
			return false
		}
	}
	return true
}

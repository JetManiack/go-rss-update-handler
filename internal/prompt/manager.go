package prompt

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

//go:embed builtin
var embedded embed.FS

// blueprint is a single prompt: metadata plus system/user templates.
type blueprint struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Critical    bool   `yaml:"critical"`
	Description string `yaml:"description"`
	System      string `yaml:"system"`
	User        string `yaml:"user"`

	systemTmpl *template.Template
	userTmpl   *template.Template
}

// Manager loads and renders prompt blueprints.
// Embedded blueprints are always loaded; a non-empty overrideDir is layered on
// top, replacing built-ins by their `name` field. Blueprints are loaded once at
// construction.
type Manager struct {
	blueprints map[string]blueprint
}

// New returns a Manager pre-loaded with embedded blueprints. If overrideDir is
// non-empty, YAML files found there override embedded ones by name.
func New(overrideDir string) (*Manager, error) {
	m := &Manager{blueprints: make(map[string]blueprint)}
	if err := m.loadFS(embedded, "builtin"); err != nil {
		return nil, err
	}
	if overrideDir != "" {
		if err := m.loadDir(overrideDir); err != nil {
			return nil, fmt.Errorf("prompt: load override dir %q: %w", overrideDir, err)
		}
	}
	return m, nil
}

// Execute renders the named blueprint with data and returns (system, user).
func (m *Manager) Execute(_ context.Context, name string, data any) (system, user string, err error) {
	bp, ok := m.blueprints[name]
	if !ok {
		return "", "", fmt.Errorf("prompt: blueprint %q not found", name)
	}
	system, err = render(bp.systemTmpl, data)
	if err != nil {
		return "", "", fmt.Errorf("prompt: render system for %q: %w", name, err)
	}
	user, err = render(bp.userTmpl, data)
	if err != nil {
		return "", "", fmt.Errorf("prompt: render user for %q: %w", name, err)
	}
	return system, user, nil
}

func (m *Manager) loadFS(fsys fs.FS, root string) error {
	return fs.WalkDir(fsys, root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if !isYAML(path) {
			return nil
		}
		data, readErr := fs.ReadFile(fsys, path)
		if readErr != nil {
			return fmt.Errorf("prompt: read %s: %w", path, readErr)
		}
		return m.parse(path, data)
	})
}

func (m *Manager) loadDir(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// A missing override dir is not an error — run on built-ins only.
			slog.Warn("prompt: override dir does not exist, using built-ins only", "dir", dir)
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("prompt: override path %q is not a directory", dir)
	}
	return fs.WalkDir(os.DirFS(dir), ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != "." && strings.HasPrefix(d.Name(), ".") {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(filepath.Base(path), ".") || !isYAML(path) {
			return nil
		}
		data, readErr := os.ReadFile(filepath.Join(dir, path)) // #nosec G304 -- path is from WalkDir within a configured directory
		if readErr != nil {
			return fmt.Errorf("prompt: read %s: %w", path, readErr)
		}
		return m.parse(path, data)
	})
}

func (m *Manager) parse(path string, data []byte) error {
	var bp blueprint
	if err := yaml.Unmarshal(data, &bp); err != nil {
		return fmt.Errorf("prompt: parse %s: %w", path, err)
	}
	if bp.Name == "" {
		base := filepath.Base(path)
		bp.Name = strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")
	}

	var err error
	if bp.systemTmpl, err = template.New("system").Parse(bp.System); err != nil {
		if bp.Critical {
			return fmt.Errorf("prompt: invalid system template in %q: %w", bp.Name, err)
		}
		slog.Warn("prompt: invalid system template, skipping", "name", bp.Name, "err", err)
		return nil
	}
	if bp.userTmpl, err = template.New("user").Parse(bp.User); err != nil {
		if bp.Critical {
			return fmt.Errorf("prompt: invalid user template in %q: %w", bp.Name, err)
		}
		slog.Warn("prompt: invalid user template, skipping", "name", bp.Name, "err", err)
		return nil
	}

	m.blueprints[bp.Name] = bp
	slog.Debug("prompt: loaded blueprint", "name", bp.Name, "version", bp.Version)
	return nil
}

func isYAML(path string) bool {
	return strings.HasSuffix(path, ".yaml") || strings.HasSuffix(path, ".yml")
}

func render(tmpl *template.Template, data any) (string, error) {
	if tmpl == nil {
		return "", nil
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

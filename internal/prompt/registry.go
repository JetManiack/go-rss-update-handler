package prompt

import (
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed builtin/*.md
var builtinFS embed.FS

type Header struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Critical    bool   `yaml:"critical"`
	Description string `yaml:"description"`
}

type promptEntry struct {
	header   Header
	template *template.Template
}

type registry struct {
	userDir string
	mu      sync.RWMutex
	prompts map[string]promptEntry
}

// Registry defines the interface for prompt management.
type Registry interface {
	Render(name string, data any) (string, error)
}

func New(userDir string) (Registry, error) {
	r := &registry{
		userDir: userDir,
		prompts: make(map[string]promptEntry),
	}

	if err := r.reload(); err != nil {
		return nil, err
	}

	if userDir != "" {
		go r.watch()
	}

	return r, nil
}

func (r *registry) reload() error {
	newPrompts := make(map[string]promptEntry)

	// Load builtins first
	entries, err := fs.ReadDir(builtinFS, "builtin")
	if err != nil {
		return fmt.Errorf("read builtin dir: %w", err)
	}

	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		data, err := fs.ReadFile(builtinFS, "builtin/"+entry.Name())
		if err != nil {
			return fmt.Errorf("read builtin %s: %w", entry.Name(), err)
		}
		
		p, err := parsePrompt(name, data)
		if err != nil {
			return fmt.Errorf("parse builtin %s: %w", entry.Name(), err)
		}
		newPrompts[name] = p
	}

	// Override with user files
	if r.userDir != "" {
		files, err := os.ReadDir(r.userDir)
		if err == nil {
			for _, file := range files {
				if file.IsDir() || !strings.HasSuffix(file.Name(), ".md") {
					continue
				}
				name := strings.TrimSuffix(file.Name(), ".md")
				data, err := os.ReadFile(filepath.Join(r.userDir, file.Name()))
				if err != nil {
					continue // Or log
				}
				p, err := parsePrompt(name, data)
				if err != nil {
					return fmt.Errorf("parse user file %s: %w", file.Name(), err)
				}
				newPrompts[name] = p
			}
		}
	}

	r.mu.Lock()
	r.prompts = newPrompts
	r.mu.Unlock()
	return nil
}

func parsePrompt(name string, data []byte) (promptEntry, error) {
	s := string(data)
	parts := strings.SplitN(s, "---", 3)
	if len(parts) < 3 {
		return promptEntry{}, fmt.Errorf("missing yaml header")
	}

	var h Header
	if err := yaml.Unmarshal([]byte(parts[1]), &h); err != nil {
		return promptEntry{}, fmt.Errorf("unmarshal yaml: %w", err)
	}

	if h.Name != name {
		return promptEntry{}, fmt.Errorf("header name %q does not match filename %q", h.Name, name)
	}
	if h.Version == "" {
		return promptEntry{}, fmt.Errorf("missing version")
	}

	tmpl, err := template.New(name).Parse(strings.TrimSpace(parts[2]))
	if err != nil {
		return promptEntry{}, fmt.Errorf("parse template: %w", err)
	}

	return promptEntry{header: h, template: tmpl}, nil
}

func (r *registry) Render(name string, data any) (string, error) {
	r.mu.RLock()
	p, ok := r.prompts[name]
	r.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("prompt not found: %s", name)
	}

	var buf strings.Builder
	if err := p.template.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render %s: %w", name, err)
	}
	return buf.String(), nil
}

func (r *registry) watch() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		if err := r.reload(); err != nil {
			fmt.Printf("reload error: %v\n", err)
		}
	}
}

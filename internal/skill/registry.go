package skill

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Registry holds loaded skills keyed by name.
// Thread-safe; safe for concurrent Get while Load methods run serially.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]*Skill
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]*Skill)}
}

// LoadBundled loads all skills embedded in the binary.
// Later calls to LoadDir or LoadBundled can override bundled skills by name.
func (r *Registry) LoadBundled() error {
	skills, err := BundledSkills()
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, s := range skills {
		r.skills[s.Frontmatter.Name] = s
	}
	return nil
}

// LoadDir scans dir for *.md files and loads each as a skill with the given source tag.
// Non-existent directories are silently skipped.
// Skills loaded later override earlier ones with the same name.
func (r *Registry) LoadDir(dir, source string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil
		}
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		s, err := Parse(data, source)
		if err != nil {
			// Skip unparseable files rather than aborting the whole load.
			continue
		}
		s.FilePath = path
		r.skills[s.Frontmatter.Name] = s
	}
	return nil
}

// Get returns the skill with the given name, or nil if not found.
func (r *Registry) Get(name string) *Skill {
	if r == nil {
		return nil
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.skills[name]
}

// Names returns all skill names in sorted order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	names := make([]string, 0, len(r.skills))
	for name := range r.skills {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// All returns all skills sorted by name.
func (r *Registry) All() []*Skill {
	r.mu.RLock()
	defer r.mu.RUnlock()
	skills := make([]*Skill, 0, len(r.skills))
	for _, s := range r.skills {
		skills = append(skills, s)
	}
	sort.Slice(skills, func(i, j int) bool {
		return skills[i].Frontmatter.Name < skills[j].Frontmatter.Name
	})
	return skills
}

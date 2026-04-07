package skill

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed skills/*.md
var bundledFS embed.FS

// BundledSkills parses and returns all skills embedded in the binary.
func BundledSkills() ([]*Skill, error) {
	entries, err := fs.ReadDir(bundledFS, "skills")
	if err != nil {
		return nil, fmt.Errorf("skill: reading bundled skills: %w", err)
	}

	skills := make([]*Skill, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		data, err := bundledFS.ReadFile("skills/" + entry.Name())
		if err != nil {
			return nil, fmt.Errorf("skill: reading bundled file %q: %w", entry.Name(), err)
		}
		s, err := Parse(data, "bundled")
		if err != nil {
			return nil, fmt.Errorf("skill: parsing bundled file %q: %w", entry.Name(), err)
		}
		skills = append(skills, s)
	}
	return skills, nil
}

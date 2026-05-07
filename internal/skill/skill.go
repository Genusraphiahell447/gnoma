package skill

import (
	"bytes"
	"fmt"
	"regexp"

	"gopkg.in/yaml.v3"
)

// namePattern validates skill names: lowercase letters, digits, hyphens, underscores.
// Must start with a letter. No spaces, slashes, or uppercase.
var namePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]*$`)

// Frontmatter holds the YAML metadata from a skill file's front matter block.
// allowedTools and paths are parsed but not enforced until M8.3.
type Frontmatter struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description"`
	WhenToUse    string   `yaml:"whenToUse"`
	AllowedTools []string `yaml:"allowedTools"`
	Paths        []string `yaml:"paths"`
}

// Skill is a parsed skill definition ready for invocation.
type Skill struct {
	Frontmatter Frontmatter
	Body        string // markdown template body, may contain {{.Args}} etc.
	Source      string // "bundled", "user", "project"
	FilePath    string // original file path; empty for bundled skills
}

// Parse reads a markdown file with YAML front matter delimited by `---` lines.
// The file must start with `---\n`. Only the first two `---` delimiters are
// treated as front matter boundaries; subsequent `---` in the body are untouched.
func Parse(data []byte, source string) (*Skill, error) {
	// File must start with the opening delimiter.
	if !bytes.HasPrefix(data, []byte("---\n")) {
		return nil, fmt.Errorf("skill: missing YAML front matter (file must start with ---)")
	}

	// Find the closing `---` delimiter. We look for `\n---\n` (or `\n---` at EOF)
	// after the opening `---\n`, so we skip the first 4 bytes.
	rest := data[4:]
	sep := []byte("\n---\n")
	idx := bytes.Index(rest, sep)
	var yamlBytes, bodyBytes []byte
	if idx >= 0 {
		yamlBytes = rest[:idx]
		bodyBytes = rest[idx+len(sep):]
	} else {
		// Try `\n---` at end of file (no trailing newline after closing delimiter).
		sep2 := []byte("\n---")
		idx2 := bytes.LastIndex(rest, sep2)
		if idx2 >= 0 && idx2 == len(rest)-len(sep2) {
			yamlBytes = rest[:idx2]
			bodyBytes = nil
		} else {
			return nil, fmt.Errorf("skill: YAML front matter not closed (missing closing ---)")
		}
	}

	var fm Frontmatter
	if err := yaml.Unmarshal(yamlBytes, &fm); err != nil {
		return nil, fmt.Errorf("skill: invalid YAML front matter: %w", err)
	}

	if fm.Name == "" {
		return nil, fmt.Errorf("skill: front matter missing required field: name")
	}
	if !namePattern.MatchString(fm.Name) {
		return nil, fmt.Errorf("skill: invalid name %q (must match ^[a-z][a-z0-9_-]*$)", fm.Name)
	}

	return &Skill{
		Frontmatter: fm,
		Body:        string(bodyBytes),
		Source:      source,
	}, nil
}

package skill

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// TemplateData holds the variables available in skill body templates.
type TemplateData struct {
	Args        string // raw user arguments after the skill name
	Cwd         string // current working directory
	ProjectRoot string // detected project root
}

// Render executes the skill body as a Go text/template with data.
// If the body contains no template directives and Args is non-empty,
// args are appended after the body with a blank line separator.
func (s *Skill) Render(data TemplateData) (string, error) {
	t, err := template.New(s.Frontmatter.Name).Parse(s.Body)
	if err != nil {
		return "", fmt.Errorf("skill %q: template parse error: %w", s.Frontmatter.Name, err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("skill %q: template execute error: %w", s.Frontmatter.Name, err)
	}

	rendered := buf.String()

	// If the body contained no template directives, the rendered output equals
	// the original body. In that case, append args (if any) after a blank line.
	if !strings.Contains(s.Body, "{{") && data.Args != "" {
		if rendered == "" {
			return data.Args, nil
		}
		return rendered + "\n" + data.Args, nil
	}

	return rendered, nil
}

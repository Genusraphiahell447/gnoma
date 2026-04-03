package security

import (
	"math"
	"regexp"
	"strings"
)

// ScanAction determines what to do when a secret is found.
type ScanAction string

const (
	ActionRedact ScanAction = "redact"
	ActionBlock  ScanAction = "block"
	ActionWarn   ScanAction = "warn"
)

// SecretPattern defines a pattern for detecting secrets.
type SecretPattern struct {
	Name   string
	Regex  *regexp.Regexp
	Action ScanAction
}

// SecretMatch represents a detected secret in content.
type SecretMatch struct {
	Pattern string // which pattern matched
	Action  ScanAction
	Start   int
	End     int
}

// Scanner detects secrets and sensitive data in content.
type Scanner struct {
	patterns         []SecretPattern
	entropyThreshold float64
}

func NewScanner(entropyThreshold float64) *Scanner {
	if entropyThreshold <= 0 {
		entropyThreshold = 4.5
	}
	return &Scanner{
		patterns:         defaultPatterns(),
		entropyThreshold: entropyThreshold,
	}
}

// AddPattern adds a custom detection pattern.
func (s *Scanner) AddPattern(name, regex string, action ScanAction) error {
	re, err := regexp.Compile(regex)
	if err != nil {
		return err
	}
	s.patterns = append(s.patterns, SecretPattern{
		Name:   name,
		Regex:  re,
		Action: action,
	})
	return nil
}

// Scan checks content for secrets. Returns all matches found.
func (s *Scanner) Scan(content string) []SecretMatch {
	var matches []SecretMatch
	seen := make(map[string]bool) // deduplicate by position

	for _, p := range s.patterns {
		locs := p.Regex.FindAllStringIndex(content, -1)
		for _, loc := range locs {
			key := strings.Join([]string{p.Name, string(rune(loc[0])), string(rune(loc[1]))}, ":")
			if seen[key] {
				continue
			}
			seen[key] = true
			matches = append(matches, SecretMatch{
				Pattern: p.Name,
				Action:  p.Action,
				Start:   loc[0],
				End:     loc[1],
			})
		}
	}

	// Entropy-based detection for unknown secret formats
	matches = append(matches, s.scanEntropy(content)...)

	return matches
}

// HasSecrets returns true if any secrets are detected.
func (s *Scanner) HasSecrets(content string) bool {
	return len(s.Scan(content)) > 0
}

// scanEntropy detects high-entropy strings that might be secrets.
func (s *Scanner) scanEntropy(content string) []SecretMatch {
	var matches []SecretMatch
	// Check each word-like token that's long enough to be a secret
	words := entropyTokenize(content)
	for _, w := range words {
		if len(w.text) < 20 { // secrets are typically 20+ chars
			continue
		}
		entropy := shannonEntropy(w.text)
		if entropy >= s.entropyThreshold {
			matches = append(matches, SecretMatch{
				Pattern: "high_entropy",
				Action:  ActionWarn,
				Start:   w.start,
				End:     w.start + len(w.text),
			})
		}
	}
	return matches
}

type token struct {
	text  string
	start int
}

func entropyTokenize(s string) []token {
	var tokens []token
	start := -1
	for i, r := range s {
		isTokenChar := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '_' || r == '-' || r == '/'
		if isTokenChar {
			if start == -1 {
				start = i
			}
		} else {
			if start != -1 {
				tokens = append(tokens, token{text: s[start:i], start: start})
				start = -1
			}
		}
	}
	if start != -1 {
		tokens = append(tokens, token{text: s[start:], start: start})
	}
	return tokens
}

// shannonEntropy calculates the Shannon entropy of a string.
func shannonEntropy(s string) float64 {
	if len(s) == 0 {
		return 0
	}
	freq := make(map[rune]float64)
	for _, r := range s {
		freq[r]++
	}
	n := float64(len([]rune(s)))
	var entropy float64
	for _, count := range freq {
		p := count / n
		if p > 0 {
			entropy -= p * math.Log2(p)
		}
	}
	return entropy
}

// defaultPatterns returns gitleaks-derived patterns for common secret formats.
func defaultPatterns() []SecretPattern {
	patterns := []struct {
		name  string
		regex string
	}{
		// Anthropic
		{"anthropic_api_key", `sk-ant-(?:api)?[a-zA-Z0-9_-]{20,}`},
		// OpenAI
		{"openai_api_key", `sk-(?:proj-)?[a-zA-Z0-9_-]{20,}`},
		// Google
		{"google_api_key", `AIza[a-zA-Z0-9_-]{35}`},
		// AWS
		{"aws_access_key", `(?:AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16}`},
		{"aws_secret_key", `(?i)aws_secret_access_key\s*=\s*[a-zA-Z0-9/+=]{40}`},
		// GitHub
		{"github_pat", `gh[pousr]_[a-zA-Z0-9]{36,}`},
		{"github_fine_grained", `github_pat_[a-zA-Z0-9]{22}_[a-zA-Z0-9]{59}`},
		// GitLab
		{"gitlab_pat", `glpat-[a-zA-Z0-9_-]{20,}`},
		// Slack
		{"slack_token", `xox[bpears]-[a-zA-Z0-9-]{10,}`},
		// Stripe
		{"stripe_key", `(?:sk|pk)_(?:live|test)_[a-zA-Z0-9]{24,}`},
		// Private keys
		{"private_key", `-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`},
		// Generic secrets in assignments
		{"generic_secret_assign", `(?i)(?:password|secret|token|api_key|apikey|auth)\s*[:=]\s*['"][a-zA-Z0-9_/+=\-]{8,}['"]`},
		// Mistral
		{"mistral_api_key", `[a-zA-Z0-9]{32}` + `(?:` + `[a-zA-Z0-9]{0}` + `)`}, // 32-char hex-like strings caught by entropy
		// Database URLs with credentials
		{"database_url", `(?i)(?:postgres|mysql|mongodb|redis)://[^:]+:[^@]+@`},
		// .env file patterns
		{"env_secret", `(?i)^[A-Z_]{2,}(?:_KEY|_SECRET|_TOKEN|_PASSWORD)\s*=\s*.{8,}$`},
	}

	var result []SecretPattern
	for _, p := range patterns {
		re, err := regexp.Compile(p.regex)
		if err != nil {
			continue // skip invalid patterns
		}
		result = append(result, SecretPattern{
			Name:   p.name,
			Regex:  re,
			Action: ActionRedact,
		})
	}
	return result
}

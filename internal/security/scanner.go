package security

import (
	"fmt"
	"math"
	"regexp"
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
			key := fmt.Sprintf("%s:%d:%d", p.Name, loc[0], loc[1])
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
		// --- AI/LLM Providers ---
		{"anthropic_api_key", `sk-ant-(?:api)?[a-zA-Z0-9_-]{20,}`},
		{"anthropic_admin_key", `sk-ant-admin[a-zA-Z0-9_-]{20,}`},
		{"openai_api_key", `sk-(?:proj-)?[a-zA-Z0-9_-]{20,}`},
		{"openai_svcacct_key", `sk-svcacct-[a-zA-Z0-9_-]{20,}`},
		{"openai_admin_key", `sk-admin-[a-zA-Z0-9_-]{20,}`},
		{"mistral_api_key", `[a-zA-Z0-9]{32}(?:[a-zA-Z0-9]{0})`}, // 32-char; entropy-gated
		{"huggingface_token", `hf_[a-zA-Z0-9]{34,}`},

		// --- Cloud Providers ---
		{"google_api_key", `AIza[a-zA-Z0-9_-]{35}`},
		{"aws_access_key", `(?:AKIA|ASIA|ABIA|ACCA)[A-Z0-9]{16}`},
		{"aws_secret_key", `(?i)aws_secret_access_key\s*=\s*[a-zA-Z0-9/+=]{40}`},
		{"azure_storage_key", `(?i)AccountKey=[a-zA-Z0-9+/=]{88}`},
		{"digitalocean_pat", `dop_v1_[a-f0-9]{64}`},
		{"digitalocean_oauth", `doo_v1_[a-f0-9]{64}`},
		{"digitalocean_refresh", `dor_v1_[a-f0-9]{64}`},
		{"vault_token", `hvs\.[a-zA-Z0-9_-]{24,}`},
		{"supabase_key", `sbp_[a-f0-9]{40}`},

		// --- Version Control ---
		{"github_pat", `gh[pousr]_[a-zA-Z0-9]{36,}`},
		{"github_fine_grained", `github_pat_[a-zA-Z0-9]{22}_[a-zA-Z0-9]{59}`},
		{"github_app_token", `ghs_[a-zA-Z0-9]{36}`},
		{"github_oauth_token", `gho_[a-zA-Z0-9]{36}`},
		{"github_refresh_token", `ghr_[a-zA-Z0-9]{36}`},
		{"gitlab_pat", `glpat-[a-zA-Z0-9_-]{20,}`},

		// --- Communication & Collaboration ---
		{"slack_token", `xox[bpears]-[a-zA-Z0-9-]{10,}`},
		{"twilio_api_key", `SK[a-f0-9]{32}`},
		{"sendgrid_api_key", `SG\.[a-zA-Z0-9_-]{22}\.[a-zA-Z0-9_-]{43}`},
		{"telegram_bot_token", `\d{8,10}:[a-zA-Z0-9_-]{35}`},
		{"discord_bot_token", `[MN][A-Za-z\d]{23,}\.[A-Za-z\d_-]{6}\.[A-Za-z\d_-]{27,}`},

		// --- Payment & Commerce ---
		{"stripe_key", `(?:sk|pk|rk)_(?:live|test)_[a-zA-Z0-9]{24,}`},
		{"shopify_access_token", `shpat_[a-fA-F0-9]{32}`},
		{"shopify_shared_secret", `shpss_[a-fA-F0-9]{32}`},

		// --- Package Registries & Dev Tools ---
		{"npm_token", `npm_[a-zA-Z0-9]{36}`},
		{"pypi_api_token", `pypi-[a-zA-Z0-9_-]{100,}`},
		{"databricks_token", `dapi[a-f0-9]{32}`},
		{"pulumi_access_token", `pul-[a-f0-9]{40}`},
		{"postman_api_key", `PMAK-[a-f0-9]{24}-[a-f0-9]{34}`},
		{"hashicorp_tf_token", `[a-zA-Z0-9]{14}\.atlasv1\.[a-zA-Z0-9_-]{60,}`},
		{"figma_pat", `figd_[a-zA-Z0-9_-]{40,}`},

		// --- Observability & Monitoring ---
		{"grafana_api_key", `eyJr[a-zA-Z0-9+/=]{60,}`},
		{"grafana_service_account", `glsa_[a-zA-Z0-9_]{32,}`},
		{"sentry_auth_token", `sntrys_[a-zA-Z0-9_]{50,}`},

		// --- Infrastructure ---
		{"private_key", `-----BEGIN (?:RSA |EC |DSA |OPENSSH )?PRIVATE KEY-----`},
		{"database_url", `(?i)(?:postgres|mysql|mongodb|redis)://[^:]+:[^@]+@`},
		{"heroku_api_key", `(?i)HEROKU_API_KEY\s*=\s*[a-f0-9]{8}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{4}-[a-f0-9]{12}`},
		{"mailgun_api_key", `key-[a-f0-9]{32}`},
		{"jwt_token", `eyJ[a-zA-Z0-9_-]{10,}\.eyJ[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}`},

		// --- Generic ---
		{"generic_secret_assign", `(?i)(?:password|secret|token|api_key|apikey|auth)\s*[:=]\s*['"][a-zA-Z0-9_/+=\-]{8,}['"]`},
		{"env_secret", `(?im)^[A-Z_]{2,}(?:_KEY|_SECRET|_TOKEN|_PASSWORD)\s*=\s*.{8,}$`},
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

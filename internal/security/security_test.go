package security

import (
	"strings"
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

// --- Scanner ---

func TestScanner_DetectsAnthropicKey(t *testing.T) {
	s := NewScanner(4.5, false)
	matches := s.Scan("my key is sk-ant-api03-abcdefghijklmnopqrstuvwxyz")
	if len(matches) == 0 {
		t.Error("should detect Anthropic API key")
	}
	if matches[0].Pattern != "anthropic_api_key" {
		t.Errorf("pattern = %q, want anthropic_api_key", matches[0].Pattern)
	}
}

func TestScanner_DetectsOpenAIKey(t *testing.T) {
	s := NewScanner(4.5, false)
	matches := s.Scan("key: sk-proj-abcdefghijklmnopqrstuvwxyz123456")
	if len(matches) == 0 {
		t.Error("should detect OpenAI API key")
	}
}

func TestScanner_DetectsAWSKey(t *testing.T) {
	s := NewScanner(4.5, false)
	matches := s.Scan("AKIAIOSFODNN7EXAMPLE")
	if len(matches) == 0 {
		t.Error("should detect AWS access key")
	}
	if matches[0].Pattern != "aws_access_key" {
		t.Errorf("pattern = %q", matches[0].Pattern)
	}
}

func TestScanner_DetectsGitHubPAT(t *testing.T) {
	s := NewScanner(4.5, false)
	matches := s.Scan("token: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij")
	hasGH := false
	for _, m := range matches {
		if m.Pattern == "github_pat" {
			hasGH = true
			break
		}
	}
	if !hasGH {
		t.Error("should detect GitHub PAT")
	}
}

func TestScanner_DetectsPrivateKey(t *testing.T) {
	s := NewScanner(4.5, false)
	matches := s.Scan("-----BEGIN RSA PRIVATE KEY-----\nMIIE...\n-----END RSA PRIVATE KEY-----")
	hasKey := false
	for _, m := range matches {
		if m.Pattern == "private_key" {
			hasKey = true
			break
		}
	}
	if !hasKey {
		t.Error("should detect private key header")
	}
}

// TestScanner_DetectsTruncatedPrivateKey covers the case where a log slice or
// buffered stream contains the BEGIN line and key body but the END marker is
// missing. The full-block private_key pattern doesn't match — the
// private_key_header fallback must fire so the body is still redacted.
func TestScanner_DetectsTruncatedPrivateKey(t *testing.T) {
	cases := []struct {
		name    string
		content string
	}{
		{
			name:    "header + body, no END",
			content: "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAAKCAQEAxyz...",
		},
		{
			name:    "OPENSSH header truncated",
			content: "log line\n-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXk",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := NewScanner(4.5, false)
			matches := s.Scan(tc.content)
			hasFallback := false
			hasFullBlock := false
			for _, m := range matches {
				switch m.Pattern {
				case "private_key_header":
					hasFallback = true
				case "private_key":
					hasFullBlock = true
				}
			}
			if hasFullBlock {
				t.Error("full-block private_key should not match a truncated key")
			}
			if !hasFallback {
				t.Error("private_key_header fallback should match a truncated key")
			}
			redacted := Redact(tc.content, matches)
			if strings.Contains(redacted, "MIIEowIBAAKCAQEAxyz") || strings.Contains(redacted, "b3BlbnNzaC1rZXk") {
				t.Errorf("key body leaked after redaction: %q", redacted)
			}
		})
	}
}

// TestRedact_PrivateKeyOverlap_SinglePlaceholder verifies that when both the
// full-block private_key pattern and the private_key_header fallback fire on
// the same complete key block, Redact merges the overlapping spans into one
// [REDACTED] placeholder rather than two.
func TestRedact_PrivateKeyOverlap_SinglePlaceholder(t *testing.T) {
	s := NewScanner(4.5, false)
	content := "x=" + "-----BEGIN RSA PRIVATE KEY-----\nMIIEowIBAA\n-----END RSA PRIVATE KEY-----" + ";"
	matches := s.Scan(content)
	redacted := Redact(content, matches)
	count := strings.Count(redacted, "[REDACTED]")
	if count != 1 {
		t.Errorf("expected exactly one [REDACTED] block, got %d: %q", count, redacted)
	}
	if !strings.HasPrefix(redacted, "x=[REDACTED]") || !strings.HasSuffix(redacted, ";") {
		t.Errorf("surrounding context not preserved: %q", redacted)
	}
}

func TestScanner_DetectsGenericSecret(t *testing.T) {
	s := NewScanner(4.5, false)
	matches := s.Scan(`password = "supersecretpassword123"`)
	hasGeneric := false
	for _, m := range matches {
		if m.Pattern == "generic_secret_assign" {
			hasGeneric = true
			break
		}
	}
	if !hasGeneric {
		t.Error("should detect generic secret assignment")
	}
}

func TestScanner_DetectsDatabaseURL(t *testing.T) {
	s := NewScanner(4.5, false)
	matches := s.Scan("postgres://admin:secretpass@db.example.com:5432/mydb")
	hasDB := false
	for _, m := range matches {
		if m.Pattern == "database_url" {
			hasDB = true
			break
		}
	}
	if !hasDB {
		t.Error("should detect database URL with credentials")
	}
}

func TestScanner_DetectsMistralKey(t *testing.T) {
	s := NewScanner(6.0, false)

	// Should detect Mistral key in assignment contexts.
	positives := []string{
		`MISTRAL_API_KEY=abcdefghijklmnop1234567890abcdef`,
		`mistral_key = "abcdefghijklmnop1234567890abcdef"`,
		`MISTRAL_API_KEY: abcdefghijklmnop1234567890abcdef`,
		`export MISTRAL_API_KEY='abcdefghijklmnop1234567890abcdef'`,
	}
	for _, text := range positives {
		matches := s.Scan(text)
		found := false
		for _, m := range matches {
			if m.Pattern == "mistral_api_key" {
				found = true
			}
		}
		if !found {
			t.Errorf("should detect Mistral key in: %s", text)
		}
	}

	// Should NOT false-positive on bare 32-char strings.
	negatives := []string{
		`commit abcdefabcdefabcdefabcdefabcdefab`,               // git hash
		`uuid: 550e8400e29b41d4a716446655440000`,                // UUID without dashes
		`checksum = "a1b2c3d4e5f6a7b8c9d0e1f2a3b4c5d6"`,        // generic checksum
	}
	for _, text := range negatives {
		matches := s.Scan(text)
		for _, m := range matches {
			if m.Pattern == "mistral_api_key" {
				t.Errorf("false positive on: %s", text)
			}
		}
	}
}

func TestScanner_NoFalsePositives(t *testing.T) {
	s := NewScanner(6.0, false) // high entropy threshold to avoid false positives
	safe := []string{
		"hello world",
		"func main() {}",
		"https://example.com/path",
		"go test ./...",
		"The quick brown fox jumps over the lazy dog",
	}
	for _, text := range safe {
		matches := s.Scan(text)
		if len(matches) > 0 {
			t.Errorf("false positive on %q: %v", text, matches[0].Pattern)
		}
	}
}

func TestScanner_Entropy(t *testing.T) {
	s := NewScanner(4.0, false) // lower threshold for testing

	// High entropy string (random-looking)
	matches := s.Scan("token: aB3dE5fG7hI9jK1lM3nO5pQ7rS9tU1v")
	hasEntropy := false
	for _, m := range matches {
		if m.Pattern == "high_entropy" {
			hasEntropy = true
			break
		}
	}
	if !hasEntropy {
		t.Error("should detect high entropy string")
	}
}

func TestShannonEntropy(t *testing.T) {
	tests := []struct {
		input    string
		minBits  float64
		maxBits  float64
	}{
		{"aaaa", 0, 0.1},              // very low entropy
		{"abcd", 1.9, 2.1},            // 4 unique chars = ~2 bits
		{"abcdefgh", 2.9, 3.1},        // 8 unique = ~3 bits
		{"aB3dE5fG7hI9jK", 3.5, 4.5},  // mixed case + digits
	}
	for _, tt := range tests {
		e := shannonEntropy(tt.input)
		if e < tt.minBits || e > tt.maxBits {
			t.Errorf("shannonEntropy(%q) = %.2f, want [%.1f, %.1f]", tt.input, e, tt.minBits, tt.maxBits)
		}
	}
}

// --- Redactor ---

func TestRedact_SingleMatch(t *testing.T) {
	content := `AKIAIOSFODNN7EXAMPLE is my key`
	s := NewScanner(6.0, false)
	matches := s.Scan(content)

	result := Redact(content, matches)
	if strings.Contains(result, "AKIA") {
		t.Error("should have redacted the key")
	}
	if !strings.Contains(result, "[REDACTED]") {
		t.Error("should contain [REDACTED] placeholder")
	}
}

func TestRedact_MultipleMatches(t *testing.T) {
	content := "aws: AKIAIOSFODNN7EXAMPLE github: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij"
	s := NewScanner(6.0, false)
	matches := s.Scan(content)

	result := Redact(content, matches)
	if strings.Contains(result, "AKIA") {
		t.Error("should redact AWS key")
	}
	if strings.Contains(result, "ghp_") {
		t.Error("should redact GitHub PAT")
	}
	count := strings.Count(result, "[REDACTED]")
	if count < 2 {
		t.Errorf("expected at least 2 redactions, got %d in: %q", count, result)
	}
}

func TestRedact_NoMatches(t *testing.T) {
	content := "hello world"
	result := Redact(content, nil)
	if result != content {
		t.Errorf("should return unchanged content, got %q", result)
	}
}

func TestRedact_SkipsWarnAction(t *testing.T) {
	matches := []SecretMatch{
		{Pattern: "test", Action: ActionWarn, Start: 0, End: 5},
	}
	result := Redact("hello world", matches)
	if result != "hello world" {
		t.Errorf("warn-only matches should not be redacted, got %q", result)
	}
}

// --- Unicode Sanitization ---

func TestSanitizeUnicode_Normal(t *testing.T) {
	normal := "Hello, world! 123"
	result := SanitizeUnicode(normal)
	if result != normal {
		t.Errorf("normal text should be unchanged, got %q", result)
	}
}

func TestSanitizeUnicode_StripsTags(t *testing.T) {
	// Unicode tag characters (U+E0000-U+E007F) used for ASCII smuggling
	tagged := "Hello" + string([]rune{0xE0048, 0xE0065, 0xE006C, 0xE006C, 0xE006F}) + " world"
	result := SanitizeUnicode(tagged)
	if result != "Hello world" {
		t.Errorf("should strip tag characters, got %q (len=%d)", result, len(result))
	}
}

func TestSanitizeUnicode_StripsZeroWidth(t *testing.T) {
	// Zero-width space (U+200B), zero-width joiner (U+200D)
	zwsp := "Hello\u200B\u200Dworld"
	result := SanitizeUnicode(zwsp)
	if result != "Helloworld" {
		t.Errorf("should strip zero-width characters, got %q", result)
	}
}

func TestSanitizeUnicode_StripsRTL(t *testing.T) {
	// RTL override (U+202E) used for visual spoofing
	rtl := "Hello\u202Eworld"
	result := SanitizeUnicode(rtl)
	if strings.ContainsRune(result, 0x202E) {
		t.Error("should strip RTL override character")
	}
}

func TestSanitizeUnicode_PreservesNewlines(t *testing.T) {
	multiline := "line1\nline2\ttab"
	result := SanitizeUnicode(multiline)
	if result != multiline {
		t.Errorf("should preserve newlines and tabs, got %q", result)
	}
}

func TestSanitizeUnicode_PreservesEmoji(t *testing.T) {
	emoji := "Hello 😊 world"
	result := SanitizeUnicode(emoji)
	if result != emoji {
		t.Errorf("should preserve emoji, got %q", result)
	}
}

// --- Incognito ---

func TestIncognito_DefaultOff(t *testing.T) {
	m := NewIncognitoMode()
	if m.Active() {
		t.Error("should default to inactive")
	}
	if !m.ShouldPersist() {
		t.Error("should allow persistence when not incognito")
	}
	if !m.ShouldLearn() {
		t.Error("should allow learning when not incognito")
	}
}

func TestIncognito_Activate(t *testing.T) {
	m := NewIncognitoMode()
	m.Activate()

	if !m.Active() {
		t.Error("should be active")
	}
	if m.ShouldPersist() {
		t.Error("should not persist in incognito")
	}
	if m.ShouldLearn() {
		t.Error("should not learn in incognito")
	}
	if m.ShouldLogContent() {
		t.Error("should not log content in incognito")
	}
}

func TestIncognito_Toggle(t *testing.T) {
	m := NewIncognitoMode()

	active := m.Toggle()
	if !active {
		t.Error("first toggle should activate")
	}

	active = m.Toggle()
	if active {
		t.Error("second toggle should deactivate")
	}
}

// --- Firewall ---

func TestFirewall_ScanOutgoing(t *testing.T) {
	fw := NewFirewall(FirewallConfig{
		ScanOutgoing:     true,
		EntropyThreshold: 6.0,
	})

	msgs := []message.Message{
		message.NewUserText("my key is sk-ant-api03-abcdefghijklmnopqrstuvwxyz"),
	}

	cleaned := fw.ScanOutgoingMessages(msgs)
	text := cleaned[0].TextContent()

	if strings.Contains(text, "sk-ant-") {
		t.Error("should redact Anthropic key from outgoing message")
	}
	if !strings.Contains(text, "[REDACTED]") {
		t.Errorf("should contain [REDACTED], got %q", text)
	}
}

func TestFirewall_ScanToolResult(t *testing.T) {
	fw := NewFirewall(FirewallConfig{
		ScanToolResults:  true,
		EntropyThreshold: 6.0,
	})

	result := fw.ScanToolResult("contents of .env:\nOPENAI_API_KEY=sk-proj-testkey1234567890abcdef12345")
	if strings.Contains(result, "sk-proj-") {
		t.Error("should redact key from tool result")
	}
}

func TestFirewall_DisabledScanning(t *testing.T) {
	fw := NewFirewall(FirewallConfig{
		ScanOutgoing:    false,
		ScanToolResults: false,
	})

	original := "sk-ant-api03-abcdefghijklmnopqrstuvwxyz"
	msgs := []message.Message{message.NewUserText(original)}

	cleaned := fw.ScanOutgoingMessages(msgs)
	if cleaned[0].TextContent() != original {
		t.Error("disabled scanning should pass through unchanged")
	}

	result := fw.ScanToolResult(original)
	if result != original {
		t.Error("disabled scanning should pass through tool results unchanged")
	}
}

func TestFirewall_UnicodeCleanedBeforeSecretScan(t *testing.T) {
	fw := NewFirewall(FirewallConfig{
		ScanOutgoing:     true,
		EntropyThreshold: 6.0,
	})

	// Unicode tags embedded in text
	tagged := "normal text" + string([]rune{0xE0048, 0xE0065}) + " more text"
	msgs := []message.Message{message.NewUserText(tagged)}

	cleaned := fw.ScanOutgoingMessages(msgs)
	text := cleaned[0].TextContent()
	if strings.ContainsRune(text, 0xE0048) {
		t.Error("unicode tags should be stripped")
	}
}

func TestFirewall_ActionBlockReturnsBlockedString(t *testing.T) {
	// Pattern with ActionBlock should return a blocked marker, not the original content
	fw := NewFirewall(FirewallConfig{
		ScanOutgoing:     true,
		EntropyThreshold: 3.0,
	})
	if err := fw.Scanner().AddPattern("test_block", `BLOCK_THIS_SECRET`, ActionBlock); err != nil {
		t.Fatalf("AddPattern: %v", err)
	}

	msgs := []message.Message{
		message.NewUserText("some text BLOCK_THIS_SECRET more text"),
	}
	cleaned := fw.ScanOutgoingMessages(msgs)
	text := cleaned[0].TextContent()

	if strings.Contains(text, "BLOCK_THIS_SECRET") {
		t.Error("ActionBlock content should not pass through")
	}
	if !strings.Contains(text, "[BLOCKED:") {
		t.Errorf("expected [BLOCKED: ...] marker, got %q", text)
	}
}

func TestScanner_DedupKeyNoCollision(t *testing.T) {
	// Two matches at byte offsets > 127 in the same pattern should both appear,
	// not get deduplicated because of hash collision in the key.
	s := NewScanner(3.0, false)
	// Build a string where two matches appear after offset 127
	prefix := strings.Repeat("x", 128) // push matches past offset 127
	input := prefix + "sk-ant-api03-aaaaaaaabbbbbbbbcccccccc " + prefix + "sk-ant-api03-ddddddddeeeeeeeeffffffff"
	matches := s.Scan(input)

	count := 0
	for _, m := range matches {
		if m.Pattern == "anthropic_api_key" {
			count++
		}
	}

	if count < 2 {
		t.Errorf("expected 2 distinct Anthropic key matches after offset 127, got %d (dedup key collision?)", count)
	}
}

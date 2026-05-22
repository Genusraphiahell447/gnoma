package security

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

// A real high-entropy token (random base64-ish) used as the "secret"
// in mixed-payload tests. Confirmed to score >= 4.5 with the default
// alphabet and to be long enough (>=20 chars) to enter scanEntropy.
const secretToken = "x9KqLm2pNvBz3RtYwH7Xj4QsDc8Fa6Vu"

// loweredThreshold sits below typical UUID/hash entropy (UUID v4 ≈ 3.4,
// SHA hex ≈ 3.9). The plan flags this regime — lowered threshold or
// redact_high_entropy = true — as where FPs bite. F-1 must remove them.
const loweredThreshold = 3.0

func TestSafelist_UUIDIsSkipped(t *testing.T) {
	s := NewScanner(loweredThreshold, true)
	s.SetSafelist([]string{"uuid"})

	matches := s.Scan("trace_id=550e8400-e29b-41d4-a716-446655440000 done")
	for _, m := range matches {
		if m.Pattern == "high_entropy" {
			t.Errorf("UUID should not be flagged as high_entropy: %+v", m)
		}
	}
}

func TestSafelist_SHA256IsSkipped(t *testing.T) {
	s := NewScanner(4.5, true)
	s.SetSafelist([]string{"sha_hex"})

	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	matches := s.Scan("commit " + sha256)
	for _, m := range matches {
		if m.Pattern == "high_entropy" {
			t.Errorf("SHA-256 should not be flagged as high_entropy: %+v", m)
		}
	}
}

func TestSafelist_SHA1IsSkipped(t *testing.T) {
	s := NewScanner(4.5, true)
	s.SetSafelist([]string{"sha_hex"})

	sha1 := "356a192b7913b04c54574d18c28d46e6395428ab"
	matches := s.Scan("blob " + sha1)
	for _, m := range matches {
		if m.Pattern == "high_entropy" {
			t.Errorf("SHA-1 should not be flagged as high_entropy: %+v", m)
		}
	}
}

func TestSafelist_MixedPayload_SecretStillCaught(t *testing.T) {
	s := NewScanner(loweredThreshold, true)
	s.SetSafelist([]string{"uuid", "sha_hex"})

	uuid := "550e8400-e29b-41d4-a716-446655440000"
	content := "id=" + uuid + " secret=" + secretToken

	matches := s.Scan(content)

	var entropyHits []SecretMatch
	for _, m := range matches {
		if m.Pattern == "high_entropy" {
			entropyHits = append(entropyHits, m)
		}
	}
	if len(entropyHits) != 1 {
		t.Fatalf("want 1 entropy hit (the actual secret), got %d: %+v", len(entropyHits), entropyHits)
	}
	// Confirm the hit covers the secret, not the UUID.
	hit := content[entropyHits[0].Start:entropyHits[0].End]
	if hit != secretToken {
		t.Errorf("entropy hit covered %q, want %q", hit, secretToken)
	}
}

func TestSafelist_EmptyPreservesCurrentBehavior(t *testing.T) {
	// No safelist configured — under a lowered threshold the UUID trips
	// entropy. This is the pre-F-1 false positive the safelist removes;
	// here we lock in that pre-F-1 behaviour is unchanged when no safelist
	// is supplied.
	s := NewScanner(loweredThreshold, true) // SetSafelist intentionally not called

	uuid := "550e8400-e29b-41d4-a716-446655440000"
	matches := s.Scan(uuid)

	var entropyHits int
	for _, m := range matches {
		if m.Pattern == "high_entropy" {
			entropyHits++
		}
	}
	if entropyHits == 0 {
		t.Error("with no safelist + lowered threshold, UUID should still trigger entropy (pre-F-1 baseline)")
	}
}

func TestSafelist_UnknownNameIgnored(t *testing.T) {
	s := NewScanner(loweredThreshold, true)
	// "made_up" is not a known pattern — must be silently dropped, not panic.
	s.SetSafelist([]string{"uuid", "made_up", "sha_hex"})

	uuid := "550e8400-e29b-41d4-a716-446655440000"
	matches := s.Scan(uuid)
	for _, m := range matches {
		if m.Pattern == "high_entropy" {
			t.Errorf("uuid should still be skipped despite unknown name in list: %+v", m)
		}
	}
}

func TestSafelist_URLPathNotFlagged(t *testing.T) {
	s := NewScanner(4.5, true)
	s.SetSafelist([]string{"url"})

	// A high-entropy URL path — a real-world false positive shape.
	url := "https://example.com/" + secretToken
	matches := s.Scan(url)
	for _, m := range matches {
		if m.Pattern == "high_entropy" {
			hit := url[m.Start:m.End]
			t.Errorf("URL substring %q should be covered by url safelist", hit)
		}
	}
}

func TestSafelist_ISO8601Span(t *testing.T) {
	// ISO-8601 timestamps don't survive entropy tokenization as a single
	// 20+-char token (':' splits them), so this is mostly a sanity check
	// that declaring iso8601 doesn't break anything.
	s := NewScanner(4.5, true)
	s.SetSafelist([]string{"iso8601"})

	ts := "2026-05-22T10:30:00.123Z"
	matches := s.Scan(ts)
	for _, m := range matches {
		if m.Pattern == "high_entropy" {
			t.Errorf("ISO-8601 timestamp should not trip entropy: %+v", m)
		}
	}
}

func TestSafelist_SecretAdjacentToUUIDStillRedacted(t *testing.T) {
	// Regression guard: a real secret that happens to abut a UUID must
	// not be swallowed by the UUID's safelist span.
	s := NewScanner(loweredThreshold, true)
	s.SetSafelist([]string{"uuid"})

	uuid := "550e8400-e29b-41d4-a716-446655440000"
	content := uuid + " " + secretToken

	matches := s.Scan(content)
	var foundSecret bool
	for _, m := range matches {
		if m.Pattern == "high_entropy" && content[m.Start:m.End] == secretToken {
			foundSecret = true
		}
	}
	if !foundSecret {
		t.Errorf("secret adjacent to UUID was not detected; matches=%+v", matches)
	}
}

func TestSafelist_KnownPatternNamesMatchPlan(t *testing.T) {
	// Plan-locked names that the user-facing TOML knob accepts.
	// Changing these breaks user configs — bump with care.
	want := []string{"uuid", "sha_hex", "iso8601", "url"}
	got := defaultSafelistPatterns()
	if len(got) != len(want) {
		t.Fatalf("default safelist size = %d, want %d", len(got), len(want))
	}
	for _, name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("missing safelist pattern %q (have %v)", name, safelistKeys(got))
		}
	}
}

func safelistKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestFirewall_EntropySafelistEndToEnd(t *testing.T) {
	// End-to-end: FirewallConfig.EntropySafelist must flow through to
	// the scanner's runtime behavior. A SHA-256 in tool output should
	// survive an entropy-redacting firewall when sha_hex is safelisted.
	sha256 := "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	content := "commit " + sha256 + " landed"

	withSafelist := NewFirewall(FirewallConfig{
		ScanToolResults:   true,
		RedactHighEntropy: true,
		EntropyThreshold:  loweredThreshold,
		EntropySafelist:   []string{"sha_hex"},
	})
	if got := withSafelist.ScanToolResult(content); !strings.Contains(got, sha256) {
		t.Errorf("safelisted SHA-256 should pass through, got %q", got)
	}

	withoutSafelist := NewFirewall(FirewallConfig{
		ScanToolResults:   true,
		RedactHighEntropy: true,
		EntropyThreshold:  loweredThreshold,
	})
	if got := withoutSafelist.ScanToolResult(content); strings.Contains(got, sha256) {
		t.Errorf("without safelist the SHA-256 should be redacted at threshold %.1f, got %q", loweredThreshold, got)
	}
}

func TestFirewall_UnknownSafelistNameWarns(t *testing.T) {
	// A typo like "uid" instead of "uuid" must surface as a Warn so the
	// operator notices, rather than silently disabling FP reduction.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	_ = NewFirewall(FirewallConfig{
		EntropySafelist: []string{"uuid", "uid"}, // "uid" is the typo
		Logger:          logger,
	})

	logs := buf.String()
	if !strings.Contains(logs, "unknown entropy safelist name") {
		t.Errorf("expected warning about unknown name, got logs: %q", logs)
	}
	if !strings.Contains(logs, "uid") {
		t.Errorf("warning should name the unknown entry, got logs: %q", logs)
	}
	if strings.Contains(logs, "name=uuid ") || strings.Contains(logs, "name=uuid\n") {
		t.Errorf("known name 'uuid' should not be warned about, got logs: %q", logs)
	}
}

func TestFirewall_AllKnownSafelistNamesQuiet(t *testing.T) {
	// No warnings for any of the canonical names — guards against a
	// future code change that accidentally renames a default pattern.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn}))

	_ = NewFirewall(FirewallConfig{
		EntropySafelist: []string{"uuid", "sha_hex", "iso8601", "url"},
		Logger:          logger,
	})

	if logs := buf.String(); logs != "" {
		t.Errorf("known safelist names should not warn, got: %q", logs)
	}
}

func TestSafelist_SkipIsLogged(t *testing.T) {
	// Per-pattern telemetry is the data F-2's go/no-go gate depends on.
	// Verify a skip emits a Debug log carrying the pattern name.
	var buf bytes.Buffer
	logger := slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug}))

	s := NewScanner(loweredThreshold, true)
	s.SetLogger(logger)
	s.SetSafelist([]string{"uuid"})

	uuid := "550e8400-e29b-41d4-a716-446655440000"
	_ = s.Scan(uuid)

	logs := buf.String()
	if !strings.Contains(logs, "entropy candidate skipped by safelist") {
		t.Errorf("expected debug log on skip, got: %q", logs)
	}
	if !strings.Contains(logs, "pattern=uuid") {
		t.Errorf("debug log should carry pattern name, got: %q", logs)
	}
}

// Sanity check the helper that powers other tests: the secret token
// we use really is high-entropy and long enough for the scanner.
func TestSafelist_SecretTokenIsHighEntropy(t *testing.T) {
	if len(secretToken) < 20 {
		t.Fatalf("secretToken too short: %d", len(secretToken))
	}
	if e := shannonEntropy(secretToken); e < 4.5 {
		t.Fatalf("secretToken entropy = %.2f, want >= 4.5 (test corpus drift)", e)
	}
	// And confirm it's stripped of any characters that would split the token.
	if strings.ContainsAny(secretToken, " .:") {
		t.Fatalf("secretToken contains a tokenizer split char")
	}
}

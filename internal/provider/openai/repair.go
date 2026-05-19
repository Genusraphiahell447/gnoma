package openai

import (
	"encoding/json"
	"regexp"
	"strings"
)

// repairArgs accepts a string of (possibly malformed) tool-call arguments and
// returns valid JSON when small lexical fixes can recover it. The bool return
// reports whether a repair was applied.
//
// Small local models served via OpenAI-compatible endpoints (Ollama,
// llama.cpp, llamafile) frequently emit args wrapped in markdown fences,
// surrounded by prose, or with trailing commas. Repairing these here keeps
// the downstream agent loop from failing on cosmetic noise.
//
// Repair tiers (cheap → less cheap):
//  1. Strict json.Valid → return as-is.
//  2. Strip ```json / ``` code fences.
//  3. Trim trailing commas before `}` or `]`.
//  4. Extract the first balanced {...} block (respects strings/escapes).
//
// If none of the tiers produces valid JSON, returns the original input bytes
// and repaired=false so callers can surface the parse error normally.
func repairArgs(raw string) (json.RawMessage, bool) {
	if raw == "" {
		return json.RawMessage(raw), false
	}
	if json.Valid([]byte(raw)) {
		return json.RawMessage(raw), false
	}

	candidates := []string{
		stripCodeFences(raw),
		stripCodeFences(trimTrailingCommas(raw)),
		extractFirstObject(raw),
		extractFirstObject(stripCodeFences(raw)),
		trimTrailingCommas(extractFirstObject(stripCodeFences(raw))),
	}
	for _, c := range candidates {
		if c == "" || c == raw {
			continue
		}
		if json.Valid([]byte(c)) {
			return json.RawMessage(c), true
		}
	}
	return json.RawMessage(raw), false
}

// codeFenceRE matches a backtick-fenced block, optionally tagged ```json.
// Captures the block's body in group 1.
var codeFenceRE = regexp.MustCompile("(?s)```(?:json)?\\s*(.*?)\\s*```")

// stripCodeFences pulls JSON out of a markdown code fence. If a complete
// fenced block exists, returns its body; otherwise strips a leading or
// trailing partial fence and returns the remainder trimmed.
func stripCodeFences(s string) string {
	if m := codeFenceRE.FindStringSubmatch(s); m != nil {
		return strings.TrimSpace(m[1])
	}
	// Partial fence — strip leading ```json / ``` if present, and any
	// trailing ``` even if no closing pair matched.
	out := s
	out = strings.TrimSpace(out)
	out = strings.TrimPrefix(out, "```json")
	out = strings.TrimPrefix(out, "```")
	out = strings.TrimSuffix(out, "```")
	return strings.TrimSpace(out)
}

// trailingCommaRE matches a comma followed only by whitespace before a
// closing `}` or `]` — i.e. a JSON-illegal trailing comma.
var trailingCommaRE = regexp.MustCompile(`,(\s*[}\]])`)

// trimTrailingCommas removes JSON-illegal trailing commas. Naïve regex
// pass — fine for our use because tool-call arg payloads don't contain
// literal commas-inside-strings that would resemble a trailing comma after
// whitespace.
func trimTrailingCommas(s string) string {
	return trailingCommaRE.ReplaceAllString(s, "$1")
}

// extractFirstObject walks s and returns the substring from the first `{`
// to its matching `}`, respecting string boundaries and escapes. Returns
// "" when no balanced object is found.
func extractFirstObject(s string) string {
	start := -1
	depth := 0
	inString := false
	escaped := false

	for i := 0; i < len(s); i++ {
		c := s[i]
		if escaped {
			escaped = false
			continue
		}
		if inString {
			switch c {
			case '\\':
				escaped = true
			case '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = true
		case '{':
			if start == -1 {
				start = i
			}
			depth++
		case '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start != -1 {
				return s[start : i+1]
			}
		}
	}
	return ""
}

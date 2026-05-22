package security

import "regexp"

// safelistEntry pairs a user-facing pattern name (the TOML knob value) with
// its compiled regex. The name flows through to log fields so operators can
// measure per-pattern FP-rate deltas — the data F-2's go/no-go decision
// depends on.
type safelistEntry struct {
	name string
	re   *regexp.Regexp
}

// safelistSpan is a half-open byte range [start, end) in the scanned content
// that the user has declared as a known-safe shape (UUID, hash, URL, timestamp).
// Tokens contained inside any span are skipped by scanEntropy — they never
// reach the entropy scorer, so they cannot produce false positives under
// lowered thresholds or redact_high_entropy = true.
type safelistSpan struct {
	start int
	end   int
	name  string
}

// defaultSafelistPatterns returns the curated allow-list of known-safe shapes,
// keyed by the user-facing name accepted in [security].entropy_safelist.
//
// Adding a key here exposes a new opt-in name to user configs. Removing or
// renaming a key is a breaking change.
func defaultSafelistPatterns() map[string]*regexp.Regexp {
	return map[string]*regexp.Regexp{
		// UUID v1–5: 8-4-4-4-12 hex with hyphens. Case-insensitive.
		"uuid": regexp.MustCompile(`(?i)\b[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\b`),

		// SHA-1 / SHA-256 / SHA-384 / SHA-512 hex digests.
		"sha_hex": regexp.MustCompile(`(?i)\b(?:[0-9a-f]{40}|[0-9a-f]{64}|[0-9a-f]{96}|[0-9a-f]{128})\b`),

		// ISO-8601 timestamp (date + time, optional fractional seconds, optional zone).
		"iso8601": regexp.MustCompile(`\b\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:?\d{2})?\b`),

		// RFC-3986-ish HTTP(S) URL. Greedy up to whitespace or quoting.
		"url": regexp.MustCompile(`\bhttps?://[^\s'"<>` + "`" + `]+`),
	}
}

// splitSafelistNames partitions user-supplied names into resolved entries and
// the list of unknown names. Callers (NewFirewall) surface unknowns so a typo
// like "uid" instead of "uuid" doesn't silently disable the safelist.
func splitSafelistNames(names []string) (entries []safelistEntry, unknown []string) {
	if len(names) == 0 {
		return nil, nil
	}
	defaults := defaultSafelistPatterns()
	for _, name := range names {
		if re, ok := defaults[name]; ok {
			entries = append(entries, safelistEntry{name: name, re: re})
		} else {
			unknown = append(unknown, name)
		}
	}
	return entries, unknown
}

// buildSafelist resolves names to entries, dropping unknowns silently. Used
// where the caller doesn't need to report typos (e.g. test setup).
func buildSafelist(names []string) []safelistEntry {
	entries, _ := splitSafelistNames(names)
	return entries
}

// safelistSpansFor returns every safelist match in content, tagged with the
// pattern name that produced it. Spans may overlap; containment is checked
// per-token in scanEntropy.
func safelistSpansFor(content string, entries []safelistEntry) []safelistSpan {
	if len(entries) == 0 {
		return nil
	}
	var spans []safelistSpan
	for _, e := range entries {
		for _, loc := range e.re.FindAllStringIndex(content, -1) {
			spans = append(spans, safelistSpan{start: loc[0], end: loc[1], name: e.name})
		}
	}
	return spans
}

// inAnySpan reports whether [start, end) lies fully inside any safelist span.
// Returns the matching pattern name so the skip can be logged for FP-rate
// telemetry — the data F-2 gates on.
func inAnySpan(spans []safelistSpan, start, end int) (string, bool) {
	for _, s := range spans {
		if start >= s.start && end <= s.end {
			return s.name, true
		}
	}
	return "", false
}

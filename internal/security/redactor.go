package security

import "sort"

const redactedPlaceholder = "[REDACTED]"

// Redact replaces detected secrets in content with [REDACTED].
// Preserves surrounding context (quotes, delimiters).
func Redact(content string, matches []SecretMatch) string {
	if len(matches) == 0 {
		return content
	}

	// Filter to redact-only and sort by start position ascending
	var redacts []SecretMatch
	for _, m := range matches {
		if m.Action == ActionRedact && m.Start >= 0 && m.End <= len(content) && m.Start < m.End {
			redacts = append(redacts, m)
		}
	}
	if len(redacts) == 0 {
		return content
	}

	sort.Slice(redacts, func(i, j int) bool {
		return redacts[i].Start < redacts[j].Start
	})

	// Merge overlapping ranges
	merged := []SecretMatch{redacts[0]}
	for _, m := range redacts[1:] {
		last := &merged[len(merged)-1]
		if m.Start <= last.End {
			// Overlapping — extend the range
			if m.End > last.End {
				last.End = m.End
			}
		} else {
			merged = append(merged, m)
		}
	}

	// Build result replacing merged ranges from end to start
	result := []byte(content)
	for i := len(merged) - 1; i >= 0; i-- {
		m := merged[i]
		replacement := []byte(redactedPlaceholder)
		result = append(result[:m.Start], append(replacement, result[m.End:]...)...)
	}
	return string(result)
}

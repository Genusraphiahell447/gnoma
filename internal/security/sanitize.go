package security

import (
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// SanitizeUnicode removes potentially dangerous invisible Unicode characters.
// Applies NFKC normalization then strips format (Cf), private use (Co),
// and unassigned (Cn) characters. Prevents ASCII smuggling and hidden
// prompt injection attacks.
func SanitizeUnicode(s string) string {
	// Step 1: NFKC normalization (handles composed characters)
	s = norm.NFKC.String(s)

	// Step 2: Strip dangerous Unicode categories
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if shouldStrip(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func shouldStrip(r rune) bool {
	// Keep normal printable characters, whitespace, and common symbols
	if r <= 0x7E && r >= 0x20 {
		return false // ASCII printable
	}
	if r == '\n' || r == '\t' || r == '\r' {
		return false // common whitespace
	}

	// Strip Unicode format characters (Cf) — invisible formatting
	if unicode.Is(unicode.Cf, r) {
		return true
	}
	// Strip private use (Co) — unregistered characters
	if unicode.Is(unicode.Co, r) {
		return true
	}

	// Strip specific dangerous ranges
	switch {
	case r >= 0xE0000 && r <= 0xE007F: // Unicode Tag characters (ASCII smuggling)
		return true
	case r >= 0xFFF0 && r <= 0xFFFD: // Specials (interlinear annotation, etc.)
		return true
	}

	return false
}

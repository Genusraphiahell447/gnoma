package tokenizer_test

import (
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/tokenizer"
)

func TestTokenizer_CountKnownText(t *testing.T) {
	tok := tokenizer.New("cl100k_base")

	// "Hello world" is 2 tokens in cl100k_base
	n := tok.Count("Hello world")
	if n < 1 || n > 5 {
		t.Errorf("unexpected token count for 'Hello world': %d", n)
	}
}

func TestTokenizer_FallbackOnBadEncoding(t *testing.T) {
	tok := tokenizer.New("nonexistent_encoding_xyz")
	// Must not panic; falls back to heuristic
	n := tok.Count("some text here")
	if n <= 0 {
		t.Errorf("expected positive count, got %d", n)
	}
}

func TestForProvider_KnownProviders(t *testing.T) {
	cases := []string{"anthropic", "openai", "mistral", "google", "ollama", "llamacpp", "unknown"}
	for _, prov := range cases {
		tok := tokenizer.ForProvider(prov)
		n := tok.Count("test input")
		if n <= 0 {
			t.Errorf("provider %q: expected positive count, got %d", prov, n)
		}
	}
}

func TestTokenizer_CodeCountsReasonably(t *testing.T) {
	tok := tokenizer.New("cl100k_base")
	code := `func main() { fmt.Println("hello") }`
	n := tok.Count(code)
	// Should be between 5 and 20 tokens for this snippet
	if n < 5 || n > 20 {
		t.Errorf("code token count out of expected range: %d", n)
	}
}

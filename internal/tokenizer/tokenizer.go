package tokenizer

import (
	"log/slog"
	"sync"

	tiktoken "github.com/pkoukk/tiktoken-go"
)

// Tokenizer counts tokens using a tiktoken BPE encoding.
// Falls back to len/4 heuristic if the encoding fails to load.
type Tokenizer struct {
	encoding string
	enc      *tiktoken.Tiktoken
	mu       sync.Mutex
	loaded   bool
	warnOnce sync.Once
}

// New creates a Tokenizer for the given tiktoken encoding name (e.g. "cl100k_base").
func New(encoding string) *Tokenizer {
	return &Tokenizer{encoding: encoding}
}

// ForProvider returns a Tokenizer appropriate for the named provider.
func ForProvider(providerName string) *Tokenizer {
	switch providerName {
	case "anthropic", "openai":
		return New("cl100k_base")
	default:
		// mistral, google, ollama, llamacpp, unknown
		return New("o200k_base")
	}
}

// Count returns the number of tokens for text using the configured encoding.
// Falls back to len(text)/4 if encoding is unavailable.
func (t *Tokenizer) Count(text string) int {
	if enc := t.getEncoding(); enc != nil {
		tokens := enc.Encode(text, nil, nil)
		return len(tokens)
	}
	// heuristic fallback
	return (len(text) + 3) / 4
}

func (t *Tokenizer) getEncoding() *tiktoken.Tiktoken {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.loaded {
		return t.enc // may be nil if failed
	}
	t.loaded = true
	enc, err := tiktoken.GetEncoding(t.encoding)
	if err != nil {
		t.warnOnce.Do(func() {
			slog.Warn("tiktoken encoding unavailable, falling back to heuristic",
				"encoding", t.encoding, "error", err)
		})
		return nil
	}
	t.enc = enc
	return enc
}

package context_test

import (
	"testing"

	gnomactx "somegit.dev/Owlibou/gnoma/internal/context"
	"somegit.dev/Owlibou/gnoma/internal/tokenizer"
)

func TestTracker_CountTokensWithTokenizer(t *testing.T) {
	tok := tokenizer.New("cl100k_base")
	tr := gnomactx.NewTracker(100000)
	tr.SetTokenizer(tok)

	n := tr.CountTokens("Hello world")
	// tiktoken gives 2; heuristic gives (11+3)/4 = 3
	if n < 1 || n > 5 {
		t.Errorf("unexpected count: %d", n)
	}
}

func TestTracker_CountTokensNilTokenizerFallsBack(t *testing.T) {
	tr := gnomactx.NewTracker(100000)
	// nil tokenizer — should use heuristic
	n := tr.CountTokens("Hello world")
	if n <= 0 {
		t.Errorf("expected positive count, got %d", n)
	}
}

package provider

import "testing"

func TestDefaultRateLimits_Mistral(t *testing.T) {
	defaults := DefaultRateLimits("mistral")

	if defaults.Provider != "mistral" {
		t.Errorf("Provider = %q, want mistral", defaults.Provider)
	}

	// Wildcard should match unknown models
	rl, ok := defaults.LookupModel("some-unknown-model")
	if !ok {
		t.Fatal("wildcard should match unknown models")
	}
	if rl.RPS != 1 {
		t.Errorf("RPS = %v, want 1", rl.RPS)
	}
	if rl.TPM != 50_000 {
		t.Errorf("TPM = %d, want 50000", rl.TPM)
	}
	if rl.TokensMonth != 4_000_000 {
		t.Errorf("TokensMonth = %d, want 4000000", rl.TokensMonth)
	}

	// Specific model
	rl, ok = defaults.LookupModel("magistral-medium-2509")
	if !ok {
		t.Fatal("magistral-medium-2509 should be found")
	}
	if rl.TPM != 75_000 {
		t.Errorf("magistral TPM = %d, want 75000", rl.TPM)
	}
	if rl.TokensMonth != 1_000_000_000 {
		t.Errorf("magistral TokensMonth = %d, want 1000000000", rl.TokensMonth)
	}
}

func TestDefaultRateLimits_Anthropic(t *testing.T) {
	defaults := DefaultRateLimits("anthropic")

	rl, ok := defaults.LookupModel("claude-sonnet-4-20250514")
	if !ok {
		t.Fatal("claude-sonnet should be found")
	}
	if rl.RPM != 50 {
		t.Errorf("RPM = %d, want 50", rl.RPM)
	}
	if rl.ITPM != 30_000 {
		t.Errorf("ITPM = %d, want 30000", rl.ITPM)
	}
	if rl.OTPM != 8_000 {
		t.Errorf("OTPM = %d, want 8000", rl.OTPM)
	}

	// Haiku has different limits
	rl, _ = defaults.LookupModel("claude-haiku-4-5-20251001")
	if rl.ITPM != 50_000 {
		t.Errorf("Haiku ITPM = %d, want 50000", rl.ITPM)
	}
}

func TestDefaultRateLimits_OpenAI(t *testing.T) {
	defaults := DefaultRateLimits("openai")

	rl, ok := defaults.LookupModel("gpt-4o")
	if !ok {
		t.Fatal("gpt-4o should be found")
	}
	if rl.RPM != 500 {
		t.Errorf("RPM = %d, want 500", rl.RPM)
	}
	if rl.TPM != 30_000 {
		t.Errorf("TPM = %d, want 30000", rl.TPM)
	}
	if rl.RPD != 10_000 {
		t.Errorf("RPD = %d, want 10000", rl.RPD)
	}
}

func TestDefaultRateLimits_Google(t *testing.T) {
	defaults := DefaultRateLimits("google")

	rl, ok := defaults.LookupModel("gemini-2.5-pro")
	if !ok {
		t.Fatal("gemini-2.5-pro should be found")
	}
	if rl.RPM != 5 {
		t.Errorf("RPM = %d, want 5", rl.RPM)
	}
	if rl.RPD != 100 {
		t.Errorf("RPD = %d, want 100", rl.RPD)
	}
}

func TestDefaultRateLimits_Unknown(t *testing.T) {
	defaults := DefaultRateLimits("unknown-provider")

	_, ok := defaults.LookupModel("any-model")
	if ok {
		t.Error("unknown provider should return no limits")
	}
}

func TestLookupModel_FallbackToWildcard(t *testing.T) {
	defaults := DefaultRateLimits("mistral")

	// Non-existent model falls back to wildcard
	rl, ok := defaults.LookupModel("mistral-future-2099")
	if !ok {
		t.Fatal("should fall back to wildcard")
	}
	if rl.TPM != 50_000 {
		t.Errorf("wildcard TPM = %d, want 50000", rl.TPM)
	}
}

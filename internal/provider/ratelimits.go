package provider

// RateLimits describes the rate limits for a provider+model pair.
// Zero values mean "no limit" or "unknown".
type RateLimits struct {
	RPS         float64 // requests per second (Mistral global)
	RPM         int     // requests per minute
	RPD         int     // requests per day
	TPM         int     // tokens per minute (combined input+output)
	ITPM        int     // input tokens per minute (Anthropic)
	OTPM        int     // output tokens per minute (Anthropic)
	TokensMonth int64   // tokens per month
	SpendCap    float64 // monthly spend cap in provider currency
}

// ProviderDefaults holds default rate limits keyed by model glob.
// The special key "*" matches any model not explicitly listed.
type ProviderDefaults struct {
	Provider string
	Tier     string // "free", "tier1", "tier2", etc.
	Models   map[string]RateLimits
}

// DefaultRateLimits returns conservative defaults for known providers.
// These are "starter tier" limits — users should override via config.
func DefaultRateLimits(providerName string) ProviderDefaults {
	switch providerName {
	case "mistral":
		return mistralDefaults()
	case "anthropic":
		return anthropicDefaults()
	case "openai":
		return openaiDefaults()
	case "google":
		return googleDefaults()
	default:
		return ProviderDefaults{Provider: providerName}
	}
}

// LookupModel finds rate limits for a specific model, falling back to "*".
func (pd ProviderDefaults) LookupModel(model string) (RateLimits, bool) {
	if rl, ok := pd.Models[model]; ok {
		return rl, true
	}
	if rl, ok := pd.Models["*"]; ok {
		return rl, true
	}
	return RateLimits{}, false
}

func mistralDefaults() ProviderDefaults {
	// Starter tier from Mistral dashboard. Spend cap is variable — not hardcoded.
	base := RateLimits{RPS: 1, TPM: 50_000, TokensMonth: 4_000_000}
	return ProviderDefaults{
		Provider: "mistral",
		Tier:     "starter",
		Models: map[string]RateLimits{
			"*": base,
			// Magistral models get higher limits
			"magistral-medium-2509": {RPS: 1, TPM: 75_000, TokensMonth: 1_000_000_000},
			"magistral-small-2509":  {RPS: 1, TPM: 75_000, TokensMonth: 1_000_000_000},
			// Large/medium get higher TPM
			"mistral-large-2411":  {RPS: 1, TPM: 600_000, TokensMonth: 200_000_000_000},
			"mistral-large-latest": {RPS: 1, TPM: 50_000, TokensMonth: 4_000_000},
			"mistral-medium-2505": {RPS: 1, TPM: 375_000},
			"mistral-medium-2508": {RPS: 1, TPM: 375_000},
			"mistral-small-2603":  {RPS: 1, TPM: 375_000},
			// Codestral
			"codestral-2508": {RPS: 1, TPM: 50_000, TokensMonth: 4_000_000},
			// Pixtral
			"pixtral-large-2411": {RPS: 1, TPM: 50_000, TokensMonth: 4_000_000},
		},
	}
}

func anthropicDefaults() ProviderDefaults {
	// Tier 1 (lowest paid tier, $5 deposit). Users on higher tiers override via config.
	return ProviderDefaults{
		Provider: "anthropic",
		Tier:     "tier1",
		Models: map[string]RateLimits{
			"*": {RPM: 50, ITPM: 30_000, OTPM: 8_000},
			// Claude 4.x Opus (shared across 4, 4.1, 4.5, 4.6)
			"claude-opus-4-20250514":     {RPM: 50, ITPM: 30_000, OTPM: 8_000},
			"claude-opus-4-0":            {RPM: 50, ITPM: 30_000, OTPM: 8_000},
			// Claude 4.x Sonnet (shared across 4, 4.5, 4.6)
			"claude-sonnet-4-20250514":   {RPM: 50, ITPM: 30_000, OTPM: 8_000},
			"claude-sonnet-4-0":          {RPM: 50, ITPM: 30_000, OTPM: 8_000},
			// Haiku
			"claude-haiku-4-5-20251001":  {RPM: 50, ITPM: 50_000, OTPM: 10_000},
			"claude-3-5-haiku-20241022":  {RPM: 50, ITPM: 50_000, OTPM: 10_000},
		},
	}
}

func openaiDefaults() ProviderDefaults {
	// Tier 1 ($5 paid). Higher tiers have dramatically higher limits.
	return ProviderDefaults{
		Provider: "openai",
		Tier:     "tier1",
		Models: map[string]RateLimits{
			"*":              {RPM: 500, TPM: 30_000, RPD: 10_000},
			"gpt-4o":         {RPM: 500, TPM: 30_000, RPD: 10_000},
			"gpt-4o-mini":    {RPM: 500, TPM: 200_000, RPD: 10_000},
			"o1":             {RPM: 500, TPM: 30_000},
			"o3":             {RPM: 500, TPM: 30_000},
			"o3-mini":        {RPM: 500, TPM: 200_000},
			"o4-mini":        {RPM: 500, TPM: 200_000},
		},
	}
}

func googleDefaults() ProviderDefaults {
	// Free tier. Pay-as-you-go Tier 1 is significantly higher.
	return ProviderDefaults{
		Provider: "google",
		Tier:     "free",
		Models: map[string]RateLimits{
			"*":                       {RPM: 15, TPM: 250_000, RPD: 250},
			"gemini-2.5-pro":          {RPM: 5, TPM: 250_000, RPD: 100},
			"gemini-2.5-pro-preview-05-06": {RPM: 5, TPM: 250_000, RPD: 100},
			"gemini-2.5-flash":        {RPM: 15, TPM: 250_000, RPD: 250},
			"gemini-2.5-flash-preview-04-17": {RPM: 15, TPM: 250_000, RPD: 250},
			"gemini-2.0-flash":        {RPM: 10, RPD: 1_500},
		},
	}
}

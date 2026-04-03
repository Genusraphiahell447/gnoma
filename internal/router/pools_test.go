package router

import (
	"testing"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

func TestPoolsFromRateLimits_Mistral(t *testing.T) {
	armID := ArmID("mistral/mistral-large-latest")
	rl := provider.RateLimits{RPS: 1, TPM: 50_000, TokensMonth: 4_000_000}

	pools := PoolsFromRateLimits(armID, rl)

	// Should create: RPS, TPM, TokensMonth = 3 pools
	if len(pools) != 3 {
		t.Fatalf("len(pools) = %d, want 3", len(pools))
	}

	kinds := map[PoolKind]bool{}
	for _, p := range pools {
		kinds[p.Kind] = true
	}
	if !kinds[PoolRPS] {
		t.Error("missing RPS pool")
	}
	if !kinds[PoolTPM] {
		t.Error("missing TPM pool")
	}
	if !kinds[PoolTokensMonth] {
		t.Error("missing TokensMonth pool")
	}
}

func TestPoolsFromRateLimits_Anthropic(t *testing.T) {
	armID := ArmID("anthropic/claude-sonnet-4-20250514")
	rl := provider.RateLimits{RPM: 50, ITPM: 30_000, OTPM: 8_000}

	pools := PoolsFromRateLimits(armID, rl)

	// Should create: RPM, ITPM (as TPM), OTPM (as TPM) = 3 pools
	if len(pools) != 3 {
		t.Fatalf("len(pools) = %d, want 3", len(pools))
	}

	var rpmPool *LimitPool
	for _, p := range pools {
		if p.Kind == PoolRPM {
			rpmPool = p
		}
	}
	if rpmPool == nil {
		t.Fatal("missing RPM pool")
	}
	if rpmPool.TotalLimit != 50 {
		t.Errorf("RPM limit = %f, want 50", rpmPool.TotalLimit)
	}
}

func TestPoolsFromRateLimits_OpenAI(t *testing.T) {
	armID := ArmID("openai/gpt-4o")
	rl := provider.RateLimits{RPM: 500, TPM: 30_000, RPD: 10_000}

	pools := PoolsFromRateLimits(armID, rl)

	// Should create: RPM, RPD, TPM = 3 pools
	if len(pools) != 3 {
		t.Fatalf("len(pools) = %d, want 3", len(pools))
	}

	kinds := map[PoolKind]int{}
	for _, p := range pools {
		kinds[p.Kind]++
	}
	if kinds[PoolRPM] != 1 {
		t.Error("expected 1 RPM pool")
	}
	if kinds[PoolRPD] != 1 {
		t.Error("expected 1 RPD pool")
	}
	if kinds[PoolTPM] != 1 {
		t.Error("expected 1 TPM pool")
	}
}

func TestPoolsFromRateLimits_Empty(t *testing.T) {
	pools := PoolsFromRateLimits("test/model", provider.RateLimits{})
	if len(pools) != 0 {
		t.Errorf("empty rate limits should produce 0 pools, got %d", len(pools))
	}
}

func TestPoolsFromRateLimits_SpendCap(t *testing.T) {
	armID := ArmID("mistral/model")
	rl := provider.RateLimits{SpendCap: 20.0}

	pools := PoolsFromRateLimits(armID, rl)
	if len(pools) != 1 {
		t.Fatalf("len(pools) = %d, want 1", len(pools))
	}
	if pools[0].Kind != PoolCostMonth {
		t.Errorf("Kind = %v, want PoolCostMonth", pools[0].Kind)
	}
	if pools[0].TotalLimit != 20.0 {
		t.Errorf("TotalLimit = %f, want 20.0", pools[0].TotalLimit)
	}
}

func TestPoolsFromRateLimits_RPSResetPeriod(t *testing.T) {
	armID := ArmID("mistral/model")
	rl := provider.RateLimits{RPS: 1}

	pools := PoolsFromRateLimits(armID, rl)
	if len(pools) != 1 {
		t.Fatalf("len(pools) = %d, want 1", len(pools))
	}
	if pools[0].ResetPeriod.Seconds() != 1.0 {
		t.Errorf("RPS reset period = %v, want 1s", pools[0].ResetPeriod)
	}
}

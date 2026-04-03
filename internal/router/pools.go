package router

import (
	"fmt"
	"time"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

// PoolsFromRateLimits creates limit pools for an arm based on rate limits.
// Each non-zero limit dimension becomes a pool attached to the arm.
func PoolsFromRateLimits(armID ArmID, rl provider.RateLimits) []*LimitPool {
	now := time.Now()
	var pools []*LimitPool

	if rl.RPS > 0 {
		pools = append(pools, &LimitPool{
			ID:          fmt.Sprintf("%s/rps", armID),
			Kind:        PoolRPS,
			TotalLimit:  rl.RPS,
			ResetPeriod: time.Second,
			ResetAt:     now.Add(time.Second),
			ArmRates:    map[ArmID]float64{armID: 1.0 / 1000.0}, // 1 request per call, normalized
			ScarcityK:   3.0,
		})
	}

	if rl.RPM > 0 {
		pools = append(pools, &LimitPool{
			ID:          fmt.Sprintf("%s/rpm", armID),
			Kind:        PoolRPM,
			TotalLimit:  float64(rl.RPM),
			ResetPeriod: time.Minute,
			ResetAt:     now.Add(time.Minute),
			ArmRates:    map[ArmID]float64{armID: 1.0 / 1000.0},
			ScarcityK:   2.0,
		})
	}

	if rl.RPD > 0 {
		pools = append(pools, &LimitPool{
			ID:          fmt.Sprintf("%s/rpd", armID),
			Kind:        PoolRPD,
			TotalLimit:  float64(rl.RPD),
			ResetPeriod: 24 * time.Hour,
			ResetAt:     nextMidnight(),
			ArmRates:    map[ArmID]float64{armID: 1.0 / 1000.0},
			ScarcityK:   2.0,
		})
	}

	if rl.TPM > 0 {
		pools = append(pools, &LimitPool{
			ID:          fmt.Sprintf("%s/tpm", armID),
			Kind:        PoolTPM,
			TotalLimit:  float64(rl.TPM),
			ResetPeriod: time.Minute,
			ResetAt:     now.Add(time.Minute),
			ArmRates:    map[ArmID]float64{armID: 1.0}, // 1 token per token
			ScarcityK:   2.0,
		})
	}

	// Anthropic-style split: ITPM+OTPM. Use TPM pool kind for the more
	// restrictive one (output) since that's what throttles agentic use.
	if rl.ITPM > 0 && rl.TPM == 0 {
		pools = append(pools, &LimitPool{
			ID:          fmt.Sprintf("%s/itpm", armID),
			Kind:        PoolTPM,
			TotalLimit:  float64(rl.ITPM),
			ResetPeriod: time.Minute,
			ResetAt:     now.Add(time.Minute),
			ArmRates:    map[ArmID]float64{armID: 0.6}, // ~60% of tokens are input
			ScarcityK:   2.0,
		})
	}
	if rl.OTPM > 0 {
		pools = append(pools, &LimitPool{
			ID:          fmt.Sprintf("%s/otpm", armID),
			Kind:        PoolTPM,
			TotalLimit:  float64(rl.OTPM),
			ResetPeriod: time.Minute,
			ResetAt:     now.Add(time.Minute),
			ArmRates:    map[ArmID]float64{armID: 0.4}, // ~40% of tokens are output
			ScarcityK:   3.0, // output is more precious
		})
	}

	if rl.TokensMonth > 0 {
		pools = append(pools, &LimitPool{
			ID:          fmt.Sprintf("%s/tokens-month", armID),
			Kind:        PoolTokensMonth,
			TotalLimit:  float64(rl.TokensMonth),
			ResetPeriod: 30 * 24 * time.Hour,
			ResetAt:     nextMonthStart(),
			ArmRates:    map[ArmID]float64{armID: 1.0},
			ScarcityK:   4.0, // aggressive hoarding for monthly budgets
		})
	}

	if rl.SpendCap > 0 {
		pools = append(pools, &LimitPool{
			ID:          fmt.Sprintf("%s/spend-cap", armID),
			Kind:        PoolCostMonth,
			TotalLimit:  rl.SpendCap,
			ResetPeriod: 30 * 24 * time.Hour,
			ResetAt:     nextMonthStart(),
			ArmRates:    map[ArmID]float64{}, // cost tracked externally per arm
			ScarcityK:   4.0,
		})
	}

	return pools
}

func nextMidnight() time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
}

func nextMonthStart() time.Time {
	now := time.Now()
	if now.Month() == 12 {
		return time.Date(now.Year()+1, 1, 1, 0, 0, 0, 0, now.Location())
	}
	return time.Date(now.Year(), now.Month()+1, 1, 0, 0, 0, 0, now.Location())
}

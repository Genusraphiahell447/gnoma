package router

import (
	"math"
	"sync"
	"time"
)

// PoolKind identifies the type of resource a pool tracks.
type PoolKind int

const (
	PoolRPM         PoolKind = iota // requests per minute
	PoolRPS                         // requests per second
	PoolRPD                         // requests per day
	PoolTPM                         // tokens per minute
	PoolTPD                         // tokens per day
	PoolTokensMonth                 // tokens per month
	PoolCostMonth                   // monetary cost cap per month
	PoolCustom                      // arbitrary units
)

// LimitPool tracks a shared resource budget that arms draw from.
type LimitPool struct {
	mu sync.Mutex

	ID          string
	Kind        PoolKind
	TotalLimit  float64
	Used        float64
	Reserved    float64 // optimistically reserved for in-flight requests
	ResetPeriod time.Duration
	ResetAt     time.Time

	// Per-arm consumption rates (units per 1k tokens or per request)
	ArmRates map[ArmID]float64

	// Scarcity curve aggressiveness. k=2 gentle, k=4 aggressive hoarding.
	ScarcityK float64
}

// RemainingFraction returns the fraction of budget still available.
func (p *LimitPool) RemainingFraction() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.TotalLimit <= 0 {
		return 0
	}
	return 1.0 - (p.Used+p.Reserved)/p.TotalLimit
}

// ScarcityMultiplier returns a cost inflation factor based on remaining budget.
// As resources deplete, the multiplier increases, making the arm more expensive.
func (p *LimitPool) ScarcityMultiplier() float64 {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.scarcityMultiplierLocked()
}

func (p *LimitPool) scarcityMultiplierLocked() float64 {
	if p.TotalLimit <= 0 {
		return math.Inf(1)
	}

	f := 1.0 - (p.Used+p.Reserved)/p.TotalLimit
	if f <= 0 {
		return math.Inf(1) // exhausted
	}

	// Use-it-or-lose-it: if reset is imminent and headroom exists, discount
	hoursToReset := time.Until(p.ResetAt).Hours()
	if !p.ResetAt.IsZero() && hoursToReset > 0 && hoursToReset < 1.0 && f > 0.3 {
		return 0.5
	}

	k := p.ScarcityK
	if k <= 0 {
		k = 2.0 // gentle default
	}
	return 1.0 / math.Pow(f, k)
}

// Exhausted returns true if the pool has no remaining capacity.
func (p *LimitPool) Exhausted() bool {
	return p.RemainingFraction() <= 0
}

// CanAfford returns true if the pool can cover the projected consumption.
func (p *LimitPool) CanAfford(armID ArmID, estimatedTokens int) bool {
	p.mu.Lock()
	defer p.mu.Unlock()

	rate := p.ArmRates[armID]
	if rate == 0 {
		return true // no rate defined = no limit
	}
	projected := rate * float64(estimatedTokens) / 1000.0
	available := p.TotalLimit - p.Used - p.Reserved
	return projected <= available
}

// Reservation represents an optimistic resource reservation.
type Reservation struct {
	pool      *LimitPool
	armID     ArmID
	projected float64
	committed bool
}

// Reserve creates an optimistic reservation. Call Commit() with actual usage
// on completion, or Rollback() on failure.
func (p *LimitPool) Reserve(armID ArmID, estimatedTokens int) (*Reservation, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()

	rate := p.ArmRates[armID]
	if rate == 0 {
		return &Reservation{pool: p}, true // no limit
	}

	projected := rate * float64(estimatedTokens) / 1000.0
	available := p.TotalLimit - p.Used - p.Reserved
	if projected > available {
		return nil, false
	}

	p.Reserved += projected
	return &Reservation{
		pool:      p,
		armID:     armID,
		projected: projected,
	}, true
}

// Commit finalizes the reservation with actual consumption.
func (r *Reservation) Commit(actualTokens int) {
	if r.committed || r.pool == nil {
		return
	}
	r.committed = true
	r.pool.mu.Lock()
	defer r.pool.mu.Unlock()

	rate := r.pool.ArmRates[r.armID]
	actual := rate * float64(actualTokens) / 1000.0

	r.pool.Reserved -= r.projected
	r.pool.Used += actual
}

// Rollback releases the reservation without consumption.
func (r *Reservation) Rollback() {
	if r.committed || r.pool == nil || r.projected == 0 {
		return
	}
	r.committed = true
	r.pool.mu.Lock()
	defer r.pool.mu.Unlock()

	r.pool.Reserved -= r.projected
}

// CheckReset resets usage if the reset period has elapsed.
func (p *LimitPool) CheckReset() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.ResetAt.IsZero() && time.Now().After(p.ResetAt) {
		p.Used = 0
		p.Reserved = 0
		p.ResetAt = p.ResetAt.Add(p.ResetPeriod)
	}
}

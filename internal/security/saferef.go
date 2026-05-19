package security

import "sync/atomic"

// FirewallRef is a late-binding holder for *Firewall.
//
// Construction order in gnoma builds provider arms before the firewall
// exists. SafeProvider takes a *FirewallRef at construction time, then
// resolves the current *Firewall on each call. This lets the wiring be
// installed before NewFirewall runs without any locking on the hot path.
//
// A nil *FirewallRef or a *FirewallRef whose pointer has not been Set
// is interpreted by SafeProvider as "no firewall installed yet" —
// requests pass through unmodified.
type FirewallRef struct {
	p atomic.Pointer[Firewall]
}

// Set installs fw as the active firewall. Safe for concurrent use.
func (r *FirewallRef) Set(fw *Firewall) {
	r.p.Store(fw)
}

// Get returns the currently installed firewall, or nil if none has been
// Set. Safe for concurrent use.
func (r *FirewallRef) Get() *Firewall {
	return r.p.Load()
}

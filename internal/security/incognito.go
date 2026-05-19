package security

import "sync"

// IncognitoMode controls privacy-sensitive behavior.
// When active: no persistence, no learning, no content logging.
//
// Routing constraint (local-only) is enforced by the router, not here —
// see router.SetLocalOnly. The two states must agree, but they live on
// different types because the router owns enforcement (mutex around arm
// selection) and the firewall owns intent. TUI/CLI bootstrap is
// responsible for keeping them in sync.
type IncognitoMode struct {
	mu     sync.RWMutex
	active bool
}

func NewIncognitoMode() *IncognitoMode {
	return &IncognitoMode{}
}

func (m *IncognitoMode) Activate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = true
}

func (m *IncognitoMode) Deactivate() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = false
}

func (m *IncognitoMode) Toggle() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.active = !m.active
	return m.active
}

func (m *IncognitoMode) Active() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.active
}

// ShouldPersist returns false when incognito is active.
func (m *IncognitoMode) ShouldPersist() bool {
	return !m.Active()
}

// ShouldLearn returns false when incognito is active (no router feedback).
func (m *IncognitoMode) ShouldLearn() bool {
	return !m.Active()
}

// ShouldLogContent returns false when incognito is active.
func (m *IncognitoMode) ShouldLogContent() bool {
	return !m.Active()
}

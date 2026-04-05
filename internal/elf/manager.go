package elf

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"somegit.dev/Owlibou/gnoma/internal/engine"
	"somegit.dev/Owlibou/gnoma/internal/permission"
	"somegit.dev/Owlibou/gnoma/internal/provider"
	"somegit.dev/Owlibou/gnoma/internal/router"
	"somegit.dev/Owlibou/gnoma/internal/security"
	"somegit.dev/Owlibou/gnoma/internal/tool"
	"somegit.dev/Owlibou/gnoma/internal/tool/persist"
)

// elfMeta tracks routing metadata and pool reservations for quality feedback.
type elfMeta struct {
	armID    router.ArmID
	taskType router.TaskType
	decision router.RoutingDecision // holds pool reservations until elf completes
}

// Manager spawns, tracks, and manages elfs.
type Manager struct {
	mu          sync.RWMutex
	elfs        map[string]Elf
	meta        map[string]elfMeta // routing metadata per elf ID
	router      *router.Router
	tools       *tool.Registry
	permissions *permission.Checker
	firewall    *security.Firewall
	store       *persist.Store
	logger      *slog.Logger
}

type ManagerConfig struct {
	Router      *router.Router
	Tools       *tool.Registry
	Permissions *permission.Checker  // nil = allow all (unsafe; prefer passing parent checker)
	Firewall    *security.Firewall   // nil = no scanning
	Store       *persist.Store       // nil = no result persistence for elfs
	Logger      *slog.Logger
}

func NewManager(cfg ManagerConfig) *Manager {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{
		elfs:        make(map[string]Elf),
		meta:        make(map[string]elfMeta),
		router:      cfg.Router,
		tools:       cfg.Tools,
		permissions: cfg.Permissions,
		firewall:    cfg.Firewall,
		store:       cfg.Store,
		logger:      logger,
	}
}

// Spawn creates a new background elf with a router-selected provider.
// The elf gets its own engine, history, and tools — no shared state.
func (m *Manager) Spawn(ctx context.Context, taskType router.TaskType, prompt, systemPrompt string, maxTurns int) (Elf, error) {
	// Ask router for the best arm for this task type
	task := router.Task{
		Type:            taskType,
		RequiresTools:   true,
		Priority:        router.PriorityNormal,
		EstimatedTokens: 4000,
	}

	decision := m.router.Select(task)
	if decision.Error != nil {
		return nil, fmt.Errorf("no arm available for elf: %w", decision.Error)
	}

	arm := decision.Arm
	m.logger.Info("spawning elf",
		"arm", arm.ID,
		"task_type", taskType,
		"model", arm.ModelName,
	)

	// Resolve permissions for this elf: inherit parent mode but never prompt
	// (no TUI in elf context — prompting would deadlock).
	elfPerms := m.permissions
	if elfPerms != nil {
		elfPerms = elfPerms.WithDenyPrompt()
	}

	// Create independent engine for the elf
	eng, err := engine.New(engine.Config{
		Provider:    arm.Provider,
		Tools:       m.tools,
		Permissions: elfPerms,
		Firewall:    m.firewall,
		System:      systemPrompt,
		Model:       arm.ModelName,
		MaxTurns:    maxTurns,
		Store:       m.store,
		Logger:      m.logger,
	})
	if err != nil {
		decision.Rollback()
		return nil, fmt.Errorf("create elf engine: %w", err)
	}

	elf := SpawnBackground(eng, prompt)

	m.mu.Lock()
	m.elfs[elf.ID()] = elf
	m.meta[elf.ID()] = elfMeta{armID: arm.ID, taskType: taskType, decision: decision}
	m.mu.Unlock()

	m.logger.Info("elf spawned", "id", elf.ID(), "arm", arm.ID)
	return elf, nil
}

// ReportResult commits pool reservations and reports an elf's outcome to the router.
func (m *Manager) ReportResult(result Result) {
	m.mu.RLock()
	meta, ok := m.meta[result.ID]
	m.mu.RUnlock()

	if !ok {
		return
	}

	// Commit pool reservations with actual token consumption.
	// Cancelled/failed elfs still commit what they consumed; a zero commit is
	// safe — it just moves reserved tokens to used at rate 0.
	meta.decision.Commit(int(result.Usage.TotalTokens()))

	m.router.ReportOutcome(router.Outcome{
		ArmID:           meta.armID,
		TaskType:        meta.taskType,
		Success:         result.Status == StatusCompleted,
		Tokens:          int(result.Usage.TotalTokens()),
		Duration:        result.Duration,
		ResultFilePaths: result.ResultFilePaths,
	})
}

// SpawnWithProvider creates an elf using a specific provider (bypasses router).
func (m *Manager) SpawnWithProvider(prov provider.Provider, model, prompt, systemPrompt string, maxTurns int) (Elf, error) {
	elfPerms := m.permissions
	if elfPerms != nil {
		elfPerms = elfPerms.WithDenyPrompt()
	}
	eng, err := engine.New(engine.Config{
		Provider:    prov,
		Tools:       m.tools,
		Permissions: elfPerms,
		Firewall:    m.firewall,
		System:      systemPrompt,
		Model:       model,
		MaxTurns:    maxTurns,
		Store:       m.store,
		Logger:      m.logger,
	})
	if err != nil {
		return nil, fmt.Errorf("create elf engine: %w", err)
	}

	elf := SpawnBackground(eng, prompt)

	m.mu.Lock()
	m.elfs[elf.ID()] = elf
	m.mu.Unlock()

	m.logger.Info("elf spawned (direct)", "id", elf.ID(), "model", model)
	return elf, nil
}

// Get returns an elf by ID.
func (m *Manager) Get(id string) (Elf, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.elfs[id]
	return e, ok
}

// List returns all tracked elfs.
func (m *Manager) List() []Elf {
	m.mu.RLock()
	defer m.mu.RUnlock()
	elfs := make([]Elf, 0, len(m.elfs))
	for _, e := range m.elfs {
		elfs = append(elfs, e)
	}
	return elfs
}

// Active returns elfs that are still running.
func (m *Manager) Active() []Elf {
	m.mu.RLock()
	defer m.mu.RUnlock()
	var active []Elf
	for _, e := range m.elfs {
		if e.Status() == StatusRunning {
			active = append(active, e)
		}
	}
	return active
}

// CancelAll cancels all running elfs.
func (m *Manager) CancelAll() {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, e := range m.elfs {
		if e.Status() == StatusRunning {
			e.Cancel()
		}
	}
}

// WaitAll waits for all elfs to complete and returns their results.
func (m *Manager) WaitAll() []Result {
	elfs := m.List()
	results := make([]Result, len(elfs))
	var wg sync.WaitGroup

	for i, e := range elfs {
		wg.Add(1)
		go func(idx int, elf Elf) {
			defer wg.Done()
			results[idx] = elf.Wait()
		}(i, e)
	}

	wg.Wait()
	return results
}

// Cleanup removes completed/failed/cancelled elfs from tracking.
func (m *Manager) Cleanup() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, e := range m.elfs {
		s := e.Status()
		if s == StatusCompleted || s == StatusFailed || s == StatusCancelled {
			delete(m.elfs, id)
			delete(m.meta, id)
		}
	}
}

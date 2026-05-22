package router

import (
	"fmt"
	"strings"

	"somegit.dev/Owlibou/gnoma/internal/provider"
)

// TaskType classifies a task for routing purposes.
type TaskType int

const (
	TaskBoilerplate    TaskType = iota // simple scaffolding, templates
	TaskGeneration                     // new code creation
	TaskRefactor                       // restructuring existing code
	TaskReview                         // code review, analysis
	TaskUnitTest                       // writing tests
	TaskPlanning                       // architecture, design
	TaskOrchestration                  // multi-step coordination
	TaskSecurityReview                 // security-focused analysis
	TaskDebug                          // finding and fixing bugs
	TaskExplain                        // explaining code or concepts
)

func (t TaskType) String() string {
	switch t {
	case TaskBoilerplate:
		return "boilerplate"
	case TaskGeneration:
		return "generation"
	case TaskRefactor:
		return "refactor"
	case TaskReview:
		return "review"
	case TaskUnitTest:
		return "unit_test"
	case TaskPlanning:
		return "planning"
	case TaskOrchestration:
		return "orchestration"
	case TaskSecurityReview:
		return "security_review"
	case TaskDebug:
		return "debug"
	case TaskExplain:
		return "explain"
	default:
		return fmt.Sprintf("unknown(%d)", t)
	}
}

// Priority indicates task importance for routing decisions.
type Priority int

const (
	PriorityLow Priority = iota
	PriorityNormal
	PriorityHigh
	PriorityCritical
)

// ClassifierSource identifies which classifier produced a Task.
// Phase 4 routing decisions depend on knowing whether the SLM is actually
// firing or whether the heuristic is silently doing all the work.
type ClassifierSource int

const (
	ClassifierUnknown     ClassifierSource = iota // unset / pre-classification
	ClassifierHeuristic                           // router.HeuristicClassifier
	ClassifierSLM                                 // slm.Classifier (SLM call succeeded)
	ClassifierSLMFallback                         // slm.Classifier fell back internally (timeout, parse error)
)

func (s ClassifierSource) String() string {
	switch s {
	case ClassifierHeuristic:
		return "heuristic"
	case ClassifierSLM:
		return "slm"
	case ClassifierSLMFallback:
		return "slm_fallback"
	default:
		return "unknown"
	}
}

// Task represents a classified unit of work for routing.
type Task struct {
	Type             TaskType
	Priority         Priority
	EstimatedTokens  int
	RequiresTools    bool
	RequiresVision   bool                 // input includes inline image content; arm must advertise Capabilities.Vision
	ComplexityScore  float64              // 0-1
	RequiredEffort   provider.EffortLevel // EffortAuto = no constraint on thinking
	ExcludedArms     []ArmID              // Arms to avoid (e.g. due to recent 429 errors)
	ClassifierSource ClassifierSource     // which classifier produced this Task
}

// ValueScore computes a routing value based on priority and type.
func (t Task) ValueScore() float64 {
	base := map[Priority]float64{
		PriorityLow:      0.5,
		PriorityNormal:   1.0,
		PriorityHigh:     2.0,
		PriorityCritical: 5.0,
	}[t.Priority]

	return base * taskTypeMultiplier[t.Type]
}

var taskTypeMultiplier = map[TaskType]float64{
	TaskBoilerplate:    0.6,
	TaskGeneration:     1.0,
	TaskRefactor:       0.9,
	TaskReview:         1.1,
	TaskUnitTest:       0.8,
	TaskPlanning:       1.4,
	TaskOrchestration:  1.5,
	TaskSecurityReview: 2.0,
	TaskDebug:          1.2,
	TaskExplain:        0.7,
}

// QualityThreshold defines minimum acceptable quality for a task type.
type QualityThreshold struct {
	Minimum    float64 // below → output is harmful, never accept
	Acceptable float64 // good enough
	Target     float64 // ideal
}

// DefaultThresholds are calibrated for M4 heuristic scores (range ~0–0.85).
// M9 will replace these with bandit-derived values once quality data accumulates.
var DefaultThresholds = map[TaskType]QualityThreshold{
	TaskBoilerplate:    {0.40, 0.55, 0.70}, // any capable arm works
	TaskGeneration:     {0.45, 0.60, 0.75},
	TaskRefactor:       {0.50, 0.65, 0.78},
	TaskReview:         {0.55, 0.68, 0.80},
	TaskUnitTest:       {0.45, 0.60, 0.75},
	TaskPlanning:       {0.60, 0.72, 0.82},
	TaskOrchestration:  {0.65, 0.75, 0.83},
	TaskSecurityReview: {0.70, 0.78, 0.84}, // requires thinking or large context window
	TaskDebug:          {0.50, 0.65, 0.78},
	TaskExplain:        {0.40, 0.55, 0.72},
}

// inferEffort derives the minimum required reasoning effort from task type and complexity.
func inferEffort(task Task) provider.EffortLevel {
	switch task.Type {
	case TaskSecurityReview:
		return provider.EffortHigh
	case TaskOrchestration:
		if task.ComplexityScore >= 0.5 {
			return provider.EffortHigh
		}
		return provider.EffortMedium
	case TaskPlanning:
		if task.ComplexityScore >= 0.7 {
			return provider.EffortHigh
		}
		return provider.EffortMedium
	case TaskDebug, TaskRefactor, TaskReview:
		if task.ComplexityScore >= 0.7 {
			return provider.EffortMedium
		}
		if task.ComplexityScore >= 0.4 {
			return provider.EffortLow
		}
		return provider.EffortAuto
	case TaskGeneration:
		if task.ComplexityScore >= 0.8 {
			return provider.EffortMedium
		}
		return provider.EffortAuto
	default:
		return provider.EffortAuto
	}
}

// ClassifyTask infers a TaskType from the user's prompt using keyword heuristics.
func ClassifyTask(prompt string) Task {
	lower := strings.ToLower(prompt)

	task := Task{
		Priority:      PriorityNormal,
		RequiresTools: true, // assume tools needed by default
	}

	// Check for task type keywords (order matters — more specific/common first).
	// Orchestration is placed late: its keywords ("dispatch", "pipeline", "orchestrat")
	// appear as nouns in non-orchestration prompts (e.g. "refactor the pipeline dispatch",
	// "review the orchestration layer"). Operational task types must gate first.
	switch {
	case containsAny(lower, "security", "vulnerability", "cve", "owasp", "xss", "injection", "audit security"):
		task.Type = TaskSecurityReview
		task.Priority = PriorityHigh
	case containsAny(lower, "debug", "fix", "troubleshoot", "not working", "error", "crash", "failing", "bug"):
		task.Type = TaskDebug
	case containsAny(lower, "review", "check", "analyze", "audit", "inspect"):
		task.Type = TaskReview
	case containsAny(lower, "refactor", "restructure", "reorganize", "clean up", "simplify"):
		task.Type = TaskRefactor
	case containsAny(lower, "test", "spec", "coverage", "assert"):
		task.Type = TaskUnitTest
	case containsAny(lower, "explain", "what is", "how does", "describe", "tell me about"):
		task.Type = TaskExplain
		task.RequiresTools = false
	case containsAny(lower, "plan", "architect", "design", "strategy", "roadmap"):
		task.Type = TaskPlanning
	case containsAny(lower, "orchestrat", "coordinate", "dispatch", "pipeline",
		"fan out", "subtask", "delegate to", "spawn elf"):
		task.Type = TaskOrchestration
		task.Priority = PriorityHigh
	case containsAny(lower, "create", "implement", "build", "add", "write", "generate", "make"):
		task.Type = TaskGeneration
	case containsAny(lower, "scaffold", "boilerplate", "template", "stub", "skeleton"):
		task.Type = TaskBoilerplate
	default:
		task.Type = TaskGeneration // default
	}

	// Estimate complexity from prompt length and keywords
	task.ComplexityScore = estimateComplexity(lower)

	// Per-task-type complexity floor. A short "refactor X" prompt looks
	// trivial by word count but the task itself implies existing code and
	// non-trivial reasoning — clamping the floor up keeps such tasks out
	// of the SLM arm's MaxComplexity ceiling.
	if floor := MinComplexityForType(task.Type); task.ComplexityScore < floor {
		task.ComplexityScore = floor
	}

	// Trivial-prompt override: short, knowledge-only prompts whose task
	// type doesn't imply existing code to read or modify can run without
	// tools — making the SLM arm (ToolUse=false) feasible for genuinely
	// tiny questions like "what is 2+2?" or "explain a closure".
	if isTrivialPrompt(lower, task.Type, task.ComplexityScore) {
		task.RequiresTools = false
	}

	task.RequiredEffort = inferEffort(task)

	return task
}

// MinComplexityForType returns the inherent complexity floor for a task
// type. Tasks that imply existing code or multi-step reasoning get a
// non-zero floor so short prompts don't slip past the SLM arm's
// MaxComplexity ceiling.
func MinComplexityForType(t TaskType) float64 {
	switch t {
	case TaskSecurityReview, TaskOrchestration:
		return 0.6
	case TaskRefactor, TaskPlanning, TaskDebug:
		return 0.4
	case TaskUnitTest, TaskReview:
		return 0.35
	default:
		return 0
	}
}

// trivialEligibleTypes are the task types where a "no tools needed" verdict
// is plausible from a short prompt alone. Debug / Refactor / Review / Test /
// SecurityReview / Orchestration all imply existing code or processes to
// touch — keep RequiresTools=true even if the wording is brief.
var trivialEligibleTypes = map[TaskType]bool{
	TaskExplain:     true,
	TaskGeneration:  true,
	TaskBoilerplate: true,
}

// toolNeedingTokens name actions/objects that always require tool execution.
// Matched as whole words (string-fields), not substrings — avoids treating
// "tester" as "test".
var toolNeedingTokens = map[string]bool{
	"read": true, "write": true, "edit": true, "create": true,
	"delete": true, "remove": true, "list": true, "find": true,
	"search": true, "grep": true, "open": true, "save": true,
	"run": true, "execute": true, "compile": true, "build": true,
	"test": true, "tests": true, "install": true, "commit": true,
	"push": true, "pull": true, "diff": true, "file": true, "files": true,
}

// isTrivialPrompt is true when the prompt is short, low-complexity, of a
// type compatible with knowledge-only answers, and contains no token that
// implies a file/shell action.
func isTrivialPrompt(lower string, taskType TaskType, complexity float64) bool {
	if !trivialEligibleTypes[taskType] {
		return false
	}
	if complexity > 0.15 {
		return false
	}
	fields := strings.Fields(lower)
	if len(fields) > 12 {
		return false
	}
	for _, w := range fields {
		w = strings.Trim(w, ".,!?;:")
		if toolNeedingTokens[w] {
			return false
		}
	}
	return true
}

func containsAny(s string, keywords ...string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

func estimateComplexity(prompt string) float64 {
	score := 0.0

	// Length contributes to complexity
	words := len(strings.Fields(prompt))
	score += float64(words) / 200.0 // normalize: 200 words = 1.0

	// Complexity keywords
	complexKeywords := []string{"implement", "design", "architect", "system", "integration", "migrate", "optimize"}
	for _, kw := range complexKeywords {
		if strings.Contains(prompt, kw) {
			score += 0.15
		}
	}

	// Simple keywords reduce complexity
	simpleKeywords := []string{"rename", "format", "add field", "change name", "typo", "simple"}
	for _, kw := range simpleKeywords {
		if strings.Contains(prompt, kw) {
			score -= 0.15
		}
	}

	// Clamp to [0, 1]
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}
	return score
}

// ParseTaskTypeStrict is like ParseTaskType but reports whether the input
// matched a known type. Used by config wiring to surface typos in
// user-supplied task-type names instead of silently falling back to
// TaskGeneration.
func ParseTaskTypeStrict(s string) (TaskType, bool) {
	switch strings.ToLower(strings.ReplaceAll(s, "_", "")) {
	case "debug":
		return TaskDebug, true
	case "explain":
		return TaskExplain, true
	case "generation":
		return TaskGeneration, true
	case "refactor":
		return TaskRefactor, true
	case "unittest":
		return TaskUnitTest, true
	case "boilerplate":
		return TaskBoilerplate, true
	case "planning":
		return TaskPlanning, true
	case "orchestration":
		return TaskOrchestration, true
	case "securityreview":
		return TaskSecurityReview, true
	case "review":
		return TaskReview, true
	}
	return TaskGeneration, false
}

// ParseTaskType converts a string from an SLM JSON response to a TaskType.
// Matching is case-insensitive. Unknown strings fall back to TaskGeneration.
// Use ParseTaskTypeStrict when you need to detect typos.
func ParseTaskType(s string) TaskType {
	t, _ := ParseTaskTypeStrict(s)
	return t
}

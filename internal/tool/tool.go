package tool

import (
	"context"
	"encoding/json"
)

// Tool is the interface every tool must implement.
type Tool interface {
	// Name returns the tool's identifier (used in LLM tool schemas).
	Name() string
	// Description returns a human-readable description for the LLM.
	Description() string
	// Parameters returns the JSON Schema for the tool's input.
	Parameters() json.RawMessage
	// Execute runs the tool with the given JSON arguments.
	Execute(ctx context.Context, args json.RawMessage) (Result, error)
	// IsReadOnly returns true if the tool only reads (safe for concurrent execution).
	IsReadOnly() bool
	// IsDestructive returns true if the tool can cause irreversible changes.
	IsDestructive() bool
}

// DeferrableTool is an optional interface for tools that can be excluded
// from initial requests and loaded on demand. Reduces token overhead
// for rarely-used tools with large schemas.
type DeferrableTool interface {
	ShouldDefer() bool
}

// PathSensitiveTool is an optional interface for tools that access the filesystem.
// Engines enforcing skill path restrictions call ExtractPaths to validate each
// invocation before execution. An empty string in the returned slice means the
// tool will default to the current working directory.
type PathSensitiveTool interface {
	ExtractPaths(args json.RawMessage) []string
}

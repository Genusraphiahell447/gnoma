package session

import (
	"time"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

// Metadata holds session summary information persisted alongside messages.
type Metadata struct {
	ID           string        `json:"id"`
	Provider     string        `json:"provider"`
	Model        string        `json:"model"`
	TurnCount    int           `json:"turn_count"`
	Usage        message.Usage `json:"usage"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
	MessageCount int           `json:"message_count"`
}

// Snapshot is a complete serialisable representation of a session at a point in time.
type Snapshot struct {
	ID       string            `json:"id"`
	Metadata Metadata          `json:"metadata"`
	Messages []message.Message `json:"messages"`
}

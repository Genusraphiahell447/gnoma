package router

import (
	"context"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

// TaskClassifier classifies a user prompt into a Task for routing decisions.
// The history slice provides prior conversation context; implementations may
// ignore it (HeuristicClassifier) or use it for richer inference (SLMClassifier).
type TaskClassifier interface {
	Classify(ctx context.Context, prompt string, history []message.Message) (Task, error)
}

// HeuristicClassifier is the default classifier. It wraps the keyword-based
// ClassifyTask function and ignores conversation history.
type HeuristicClassifier struct{}

func (HeuristicClassifier) Classify(_ context.Context, prompt string, _ []message.Message) (Task, error) {
	return ClassifyTask(prompt), nil
}

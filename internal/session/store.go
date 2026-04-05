package session

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sort"

	"somegit.dev/Owlibou/gnoma/internal/message"
)

// SessionStore manages session persistence to .gnoma/sessions/.
type SessionStore struct {
	dir     string
	maxKeep int
	logger  *slog.Logger
}

// NewSessionStore creates a store rooted at <projectRoot>/.gnoma/sessions/.
func NewSessionStore(projectRoot string, maxKeep int, logger *slog.Logger) *SessionStore {
	return &SessionStore{
		dir:     filepath.Join(projectRoot, ".gnoma", "sessions"),
		maxKeep: maxKeep,
		logger:  logger,
	}
}

func (s *SessionStore) Save(snap Snapshot) error {
	dir := filepath.Join(s.dir, snap.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("session %q: create dir: %w", snap.ID, err)
	}

	if err := atomicWrite(filepath.Join(dir, "metadata.json"), snap.Metadata); err != nil {
		return fmt.Errorf("session %q: write metadata: %w", snap.ID, err)
	}

	if err := atomicWrite(filepath.Join(dir, "messages.json"), snap.Messages); err != nil {
		return fmt.Errorf("session %q: write messages: %w", snap.ID, err)
	}

	return s.Prune()
}

func (s *SessionStore) Load(id string) (Snapshot, error) {
	dir := filepath.Join(s.dir, id)

	metaBytes, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("session %q not found: %w", id, err)
	}

	var meta Metadata
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return Snapshot{}, fmt.Errorf("session %q: corrupt metadata: %w", id, err)
	}

	msgBytes, err := os.ReadFile(filepath.Join(dir, "messages.json"))
	if err != nil {
		return Snapshot{}, fmt.Errorf("session %q: read messages: %w", id, err)
	}

	var msgs []message.Message
	if err := json.Unmarshal(msgBytes, &msgs); err != nil {
		return Snapshot{}, fmt.Errorf("session %q: corrupt messages: %w", id, err)
	}

	return Snapshot{
		ID:       id,
		Metadata: meta,
		Messages: msgs,
	}, nil
}

func (s *SessionStore) List() ([]Metadata, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("list sessions: %w", err)
	}

	var result []Metadata
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		id := entry.Name()
		metaBytes, err := os.ReadFile(filepath.Join(s.dir, id, "metadata.json"))
		if err != nil {
			s.logger.Warn("session: skip unreadable metadata", "id", id, "err", err)
			continue
		}
		var meta Metadata
		if err := json.Unmarshal(metaBytes, &meta); err != nil {
			s.logger.Warn("session: skip corrupt metadata", "id", id, "err", err)
			continue
		}
		meta.ID = id
		result = append(result, meta)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].UpdatedAt.After(result[j].UpdatedAt)
	})

	return result, nil
}

func (s *SessionStore) Prune() error {
	list, err := s.List()
	if err != nil {
		return fmt.Errorf("prune: list sessions: %w", err)
	}

	if len(list) <= s.maxKeep {
		return nil
	}

	for _, meta := range list[s.maxKeep:] {
		dir := filepath.Join(s.dir, meta.ID)
		if err := os.RemoveAll(dir); err != nil {
			s.logger.Warn("session: prune failed", "id", meta.ID, "err", err)
		}
	}

	return nil
}

func atomicWrite(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("rename: %w", err)
	}
	return nil
}

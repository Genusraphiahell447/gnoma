package plugin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/BurntSushi/toml"
)

// PinStore records the trusted SHA-256 of each enrolled plugin's manifest.
// Implementations must be safe for the single-process startup flow used by
// the loader; no concurrent Set calls are issued.
type PinStore interface {
	Get(name string) (hash string, ok bool)
	Set(name, hash string) error
}

// FilePinStore persists pins to a TOML file with a single [pins] table.
type FilePinStore struct {
	path string
	pins map[string]string
}

type pinsFile struct {
	Pins map[string]string `toml:"pins"`
}

func NewFilePinStore(path string) (*FilePinStore, error) {
	s := &FilePinStore{path: path, pins: make(map[string]string)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read pin file %q: %w", path, err)
	}
	var pf pinsFile
	if _, err := toml.Decode(string(data), &pf); err != nil {
		return nil, fmt.Errorf("decode pin file %q: %w", path, err)
	}
	if pf.Pins != nil {
		s.pins = pf.Pins
	}
	return s, nil
}

func (s *FilePinStore) Get(name string) (string, bool) {
	h, ok := s.pins[name]
	return h, ok
}

func (s *FilePinStore) Set(name, hash string) error {
	s.pins[name] = hash
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return fmt.Errorf("create pin dir: %w", err)
	}
	// Atomic write: temp + rename so a crash mid-write doesn't corrupt the file.
	tmp, err := os.CreateTemp(filepath.Dir(s.path), "pins-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp pin file: %w", err)
	}
	tmpPath := tmp.Name()
	enc := toml.NewEncoder(tmp)
	encErr := enc.Encode(pinsFile{Pins: s.pins})
	closeErr := tmp.Close()
	if encErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("encode pin file: %w", encErr)
	}
	if closeErr != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close temp pin file: %w", closeErr)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("rename pin file: %w", err)
	}
	return nil
}

package slm

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

const manifestFile = "manifest.json"

// Manifest records the result of a successful slm setup.
// Presence on disk is the "ready" invariant — written only after download succeeds.
type Manifest struct {
	ModelURL string    `json:"model_url"`
	FilePath string    `json:"file_path"`
	SHA256   string    `json:"sha256"` // hex-encoded SHA256 of the downloaded file
	Size     int64     `json:"size"`
	SetupAt  time.Time `json:"setup_at"`
}

// readManifest loads the manifest from dir. Returns os.ErrNotExist if absent.
func readManifest(dir string) (*Manifest, error) {
	data, err := os.ReadFile(filepath.Join(dir, manifestFile))
	if err != nil {
		return nil, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("slm: corrupt manifest: %w", err)
	}
	return &m, nil
}

// writeManifest atomically writes m to dir, creating dir if needed.
func writeManifest(dir string, m *Manifest) error {
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("slm: create data dir: %w", err)
	}
	tmp := filepath.Join(dir, manifestFile+".tmp")
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, manifestFile))
}

package slm

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWriteAndReadManifest(t *testing.T) {
	dir := t.TempDir()
	want := &Manifest{
		ModelURL: "https://example.com/model.llamafile",
		FilePath: "/data/model.llamafile",
		SHA256:   "abc123",
		Size:     1024,
		SetupAt:  time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
	}

	if err := writeManifest(dir, want); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}

	got, err := readManifest(dir)
	if err != nil {
		t.Fatalf("readManifest: %v", err)
	}

	if got.ModelURL != want.ModelURL {
		t.Errorf("ModelURL: got %q, want %q", got.ModelURL, want.ModelURL)
	}
	if got.FilePath != want.FilePath {
		t.Errorf("FilePath: got %q, want %q", got.FilePath, want.FilePath)
	}
	if got.SHA256 != want.SHA256 {
		t.Errorf("SHA256: got %q, want %q", got.SHA256, want.SHA256)
	}
	if got.Size != want.Size {
		t.Errorf("Size: got %d, want %d", got.Size, want.Size)
	}
	if !got.SetupAt.Equal(want.SetupAt) {
		t.Errorf("SetupAt: got %v, want %v", got.SetupAt, want.SetupAt)
	}
}

func TestWriteManifest_CreatesDir(t *testing.T) {
	base := t.TempDir()
	dir := filepath.Join(base, "nested", "slm")

	m := &Manifest{ModelURL: "u", FilePath: "p", SHA256: "h", Size: 0, SetupAt: time.Now()}
	if err := writeManifest(dir, m); err != nil {
		t.Fatalf("writeManifest: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, manifestFile)); err != nil {
		t.Fatalf("manifest file not created: %v", err)
	}
}

func TestReadManifest_NotExist(t *testing.T) {
	dir := t.TempDir()
	_, err := readManifest(dir)
	if !errors.Is(err, os.ErrNotExist) {
		t.Errorf("want os.ErrNotExist, got %v", err)
	}
}

func TestReadManifest_CorruptJSON(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, manifestFile), []byte("not json"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := readManifest(dir)
	if err == nil {
		t.Fatal("want error for corrupt JSON, got nil")
	}
}

func TestWriteManifest_Atomic(t *testing.T) {
	dir := t.TempDir()
	m := &Manifest{ModelURL: "u", FilePath: "p", SHA256: "h", Size: 42, SetupAt: time.Now()}
	if err := writeManifest(dir, m); err != nil {
		t.Fatal(err)
	}
	// No tmp file should remain.
	if _, err := os.Stat(filepath.Join(dir, manifestFile+".tmp")); !os.IsNotExist(err) {
		t.Error("temp file should not exist after successful write")
	}
}

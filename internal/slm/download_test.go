package slm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownload_Success(t *testing.T) {
	content := []byte("hello llamafile")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "model.llamafile")
	var lastDownloaded, lastTotal int64
	sha256hex, size, err := download(context.Background(), srv.URL, dst, func(d, total int64) {
		lastDownloaded, lastTotal = d, total
	})
	if err != nil {
		t.Fatalf("download: %v", err)
	}

	h := sha256.Sum256(content)
	want := hex.EncodeToString(h[:])
	if sha256hex != want {
		t.Errorf("SHA256: got %s, want %s", sha256hex, want)
	}
	if size != int64(len(content)) {
		t.Errorf("size: got %d, want %d", size, len(content))
	}
	if lastDownloaded != int64(len(content)) {
		t.Errorf("progress last downloaded: got %d, want %d", lastDownloaded, len(content))
	}
	_ = lastTotal // may be -1 if server doesn't set Content-Length

	data, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read dst: %v", err)
	}
	if string(data) != string(content) {
		t.Errorf("file content mismatch")
	}
}

func TestDownload_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "model.llamafile")
	_, _, err := download(context.Background(), srv.URL, dst, nil)
	if err == nil {
		t.Fatal("want error for 404, got nil")
	}
	// Partial file must be cleaned up.
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Error("partial file should have been deleted on failure")
	}
}

func TestDownload_ContextCancel(t *testing.T) {
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			http.Error(w, "no flusher", 500)
			return
		}
		w.WriteHeader(http.StatusOK)
		flusher.Flush()
		<-done // block until test cancels
	}))
	defer srv.Close()
	defer close(done)

	dst := filepath.Join(t.TempDir(), "model.llamafile")
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, _, err := download(ctx, srv.URL, dst, nil)
	if err == nil {
		t.Fatal("want error on cancelled context, got nil")
	}
}

func TestDownload_NilProgress(t *testing.T) {
	content := []byte("data")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(content)
	}))
	defer srv.Close()

	dst := filepath.Join(t.TempDir(), "model.llamafile")
	_, _, err := download(context.Background(), srv.URL, dst, nil)
	if err != nil {
		t.Fatalf("download with nil progress: %v", err)
	}
}

func TestHashFile(t *testing.T) {
	content := []byte("test content for hashing")
	f, err := os.CreateTemp(t.TempDir(), "hash*")
	if err != nil {
		t.Fatal(err)
	}
	f.Write(content)
	f.Close()

	h := sha256.Sum256(content)
	want := hex.EncodeToString(h[:])

	got, size, err := hashFile(f.Name())
	if err != nil {
		t.Fatalf("hashFile: %v", err)
	}
	if got != want {
		t.Errorf("SHA256: got %s, want %s", got, want)
	}
	if size != int64(len(content)) {
		t.Errorf("size: got %d, want %d", size, len(content))
	}
}

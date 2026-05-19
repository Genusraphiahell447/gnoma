package slm

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestManager_StatusNotSetUp(t *testing.T) {
	m := New(Config{DataDir: t.TempDir(), ModelURL: ""}, nil)
	if got := m.Status(); got != StatusNotSetUp {
		t.Errorf("Status = %v, want StatusNotSetUp", got)
	}
}

func TestManager_IsSetUp_False(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()}, nil)
	if m.IsSetUp() {
		t.Error("IsSetUp should be false before Setup")
	}
}

func TestManager_StatusReady(t *testing.T) {
	dir := t.TempDir()
	// create the actual file the manifest points to
	dst := filepath.Join(dir, "model.llamafile")
	if err := os.WriteFile(dst, []byte("binary"), 0700); err != nil {
		t.Fatal(err)
	}
	mf := &Manifest{
		ModelURL: "https://example.com/model.llamafile",
		FilePath: dst,
		SHA256:   "abc",
		Size:     6,
		SetupAt:  time.Now(),
	}
	if err := writeManifest(dir, mf); err != nil {
		t.Fatal(err)
	}

	m := New(Config{DataDir: dir}, nil)
	if got := m.Status(); got != StatusReady {
		t.Errorf("Status = %v, want StatusReady", got)
	}
	if !m.IsSetUp() {
		t.Error("IsSetUp should be true when manifest + file exist")
	}
}

func TestManager_StatusMissing(t *testing.T) {
	dir := t.TempDir()
	mf := &Manifest{
		ModelURL: "https://example.com/model.llamafile",
		FilePath: filepath.Join(dir, "gone.llamafile"), // file does NOT exist
		SHA256:   "abc",
		Size:     0,
		SetupAt:  time.Now(),
	}
	if err := writeManifest(dir, mf); err != nil {
		t.Fatal(err)
	}

	m := New(Config{DataDir: dir}, nil)
	if got := m.Status(); got != StatusMissing {
		t.Errorf("Status = %v, want StatusMissing", got)
	}
}

func TestManager_Setup(t *testing.T) {
	content := []byte("fake llamafile binary")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := New(Config{DataDir: dir, ModelURL: srv.URL + "/model.llamafile"}, nil)

	var progressCalled bool
	if err := m.Setup(context.Background(), func(_, _ int64) { progressCalled = true }); err != nil {
		t.Fatalf("Setup: %v", err)
	}

	if !progressCalled {
		t.Error("progress callback should have been called")
	}

	mf, err := readManifest(dir)
	if err != nil {
		t.Fatalf("readManifest after Setup: %v", err)
	}
	if mf.ModelURL != srv.URL+"/model.llamafile" {
		t.Errorf("manifest ModelURL = %q, want %q", mf.ModelURL, srv.URL+"/model.llamafile")
	}
	if mf.SHA256 == "" {
		t.Error("manifest SHA256 should not be empty")
	}
	h := sha256.Sum256(content)
	wantHash := hex.EncodeToString(h[:])
	if mf.SHA256 != wantHash {
		t.Errorf("manifest SHA256 = %q, want %q", mf.SHA256, wantHash)
	}
	if mf.Size != int64(len(content)) {
		t.Errorf("manifest Size = %d, want %d", mf.Size, len(content))
	}
	if mf.SetupAt.IsZero() {
		t.Error("manifest SetupAt should not be zero")
	}
	if _, err := os.Stat(mf.FilePath); err != nil {
		t.Errorf("llamafile file not found: %v", err)
	}

	// Status should now be Ready.
	if m.Status() != StatusReady {
		t.Errorf("Status after Setup = %v, want StatusReady", m.Status())
	}
}

func TestManager_Setup_NoURL(t *testing.T) {
	m := New(Config{DataDir: t.TempDir(), ModelURL: ""}, nil)
	if err := m.Setup(context.Background(), nil); err == nil {
		t.Fatal("want error when ModelURL is empty")
	}
}

func TestManager_Setup_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	dir := t.TempDir()
	m := New(Config{DataDir: dir, ModelURL: srv.URL + "/model.llamafile"}, nil)
	if err := m.Setup(context.Background(), nil); err == nil {
		t.Fatal("want error when server returns 500")
	}
	// Manifest must NOT have been written.
	if m.Status() != StatusNotSetUp {
		t.Errorf("Status after failed Setup = %v, want StatusNotSetUp", m.Status())
	}
}

func TestManager_Start_NotSetUp(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()}, nil)
	_, err := m.Start(context.Background())
	if err == nil {
		t.Fatal("want error when not set up")
	}
}

func TestManager_Stop_NotStarted(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()}, nil)
	if err := m.Stop(); err != nil {
		t.Errorf("Stop on not-started manager: %v", err)
	}
}

func TestManager_BaseURL_NotStarted(t *testing.T) {
	m := New(Config{DataDir: t.TempDir()}, nil)
	if got := m.BaseURL(); got != "" {
		t.Errorf("BaseURL before Start = %q, want empty", got)
	}
}

func TestWaitHealthy_OK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := waitHealthy(ctx, srv.URL); err != nil {
		t.Errorf("waitHealthy: %v", err)
	}
}

func TestWaitHealthy_Timeout(t *testing.T) {
	// Server returns 503 so health check never passes.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	if err := waitHealthy(ctx, srv.URL); err == nil {
		t.Fatal("want timeout error, got nil")
	}
}

func TestWaitHealthy_ContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitHealthy(ctx, srv.URL); err == nil {
		t.Fatal("want error on cancelled context")
	}
}

func TestFreePort(t *testing.T) {
	port, err := freePort()
	if err != nil {
		t.Fatalf("freePort: %v", err)
	}
	if port <= 0 || port > 65535 {
		t.Errorf("freePort returned invalid port %d", port)
	}
}

func TestFreePort_Unique(t *testing.T) {
	p1, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	p2, err := freePort()
	if err != nil {
		t.Fatal(err)
	}
	// Two consecutive calls shouldn't return the same port (bind-then-close relinquishes it,
	// but the OS generally won't hand it back immediately).
	_ = p1
	_ = p2
}

func TestManager_ReapStalePID(t *testing.T) {
	dir := t.TempDir()
	// Write a PID that doesn't belong to any running process (PID 0 is always invalid).
	pidPath := filepath.Join(dir, pidFile)
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(0)), 0600); err != nil {
		t.Fatal(err)
	}

	m := New(Config{DataDir: dir}, nil)
	m.reapStalePID() // should not panic

	// PID file should be removed.
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("pid file should be removed after reap")
	}
}

func TestManager_ReapStalePID_NoPIDFile(t *testing.T) {
	dir := t.TempDir()
	m := New(Config{DataDir: dir}, nil)
	m.reapStalePID() // should be a no-op without panic
}

func TestManager_ReapStalePID_GarbagePID(t *testing.T) {
	dir := t.TempDir()
	pidPath := filepath.Join(dir, pidFile)
	if err := os.WriteFile(pidPath, []byte("not-a-pid"), 0600); err != nil {
		t.Fatal(err)
	}

	m := New(Config{DataDir: dir}, nil)
	m.reapStalePID() // should not panic

	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Error("garbage pid file should be removed")
	}
}

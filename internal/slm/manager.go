package slm

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const pidFile = "llamafile.pid"

// DefaultModelURL is the default llamafile to download when none is configured.
// TinyLlama 1.1B Chat Q5_K_M (~690 MB) — small enough to download quickly,
// sufficient for JSON classification tasks.
const DefaultModelURL = "https://huggingface.co/mozilla-ai/TinyLlama-1.1B-Chat-v1.0-llamafile/resolve/main/TinyLlama-1.1B-Chat-v1.0.Q5_K_M.llamafile"

// DefaultDataDir returns the platform default SLM data directory.
// Follows XDG Base Directory Specification: $XDG_DATA_HOME/gnoma/slm,
// falling back to ~/.local/share/gnoma/slm.
func DefaultDataDir() string {
	dir := os.Getenv("XDG_DATA_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(dir, "gnoma", "slm")
}

// Status describes the setup state of the SLM.
type Status int

const (
	StatusNotSetUp Status = iota // no manifest on disk
	StatusReady                  // manifest + binary file both exist
	StatusMissing                // manifest exists but binary file is gone
)

func (s Status) String() string {
	switch s {
	case StatusNotSetUp:
		return "not set up"
	case StatusReady:
		return "ready"
	case StatusMissing:
		return "file missing"
	default:
		return "unknown"
	}
}

// Config holds Manager configuration.
type Config struct {
	DataDir  string // XDG data home / gnoma / slm; must be set
	ModelURL string // required for Setup
}

// Manager controls the llamafile lifecycle.
type Manager struct {
	cfg     Config
	process *os.Process
	port    int
	logger  *slog.Logger
}

// New creates a Manager. DataDir must be non-empty.
func New(cfg Config, logger *slog.Logger) *Manager {
	if logger == nil {
		logger = slog.Default()
	}
	return &Manager{cfg: cfg, logger: logger}
}

// IsSetUp returns true when Status() == StatusReady.
func (m *Manager) IsSetUp() bool {
	return m.Status() == StatusReady
}

// Status returns the current setup state by inspecting the manifest and filesystem.
func (m *Manager) Status() Status {
	mf, err := readManifest(m.cfg.DataDir)
	if err != nil {
		return StatusNotSetUp
	}
	if _, err := os.Stat(mf.FilePath); err != nil {
		return StatusMissing
	}
	return StatusReady
}

// Setup downloads the llamafile from ModelURL, verifies the hash, and writes the manifest.
// progress receives (downloaded, total) byte counts; may be nil.
func (m *Manager) Setup(ctx context.Context, progress func(downloaded, total int64)) error {
	if m.cfg.ModelURL == "" {
		return fmt.Errorf("slm: ModelURL is required")
	}

	if m.Status() == StatusReady {
		return nil
	}

	if err := os.MkdirAll(m.cfg.DataDir, 0700); err != nil {
		return fmt.Errorf("slm: create data dir: %w", err)
	}

	name := filepath.Base(m.cfg.ModelURL)
	if name == "" || name == "." {
		name = "llamafile"
	}
	dst := filepath.Join(m.cfg.DataDir, name)

	m.logger.Info("downloading llamafile", "url", m.cfg.ModelURL, "dst", dst)

	sha256hex, size, err := download(ctx, m.cfg.ModelURL, dst, progress)
	if err != nil {
		return err
	}

	mf := &Manifest{
		ModelURL: m.cfg.ModelURL,
		FilePath: dst,
		SHA256:   sha256hex,
		Size:     size,
		SetupAt:  time.Now().UTC(),
	}
	return writeManifest(m.cfg.DataDir, mf)
}

// Start launches the llamafile subprocess and returns its base URL.
// Reaps a stale PID file from a previous run if present.
func (m *Manager) Start(ctx context.Context) (string, error) {
	mf, err := readManifest(m.cfg.DataDir)
	if err != nil {
		return "", fmt.Errorf("slm: not set up: %w", err)
	}
	if _, err := os.Stat(mf.FilePath); err != nil {
		return "", fmt.Errorf("slm: llamafile missing at %s", mf.FilePath)
	}

	m.reapStalePID()

	port, err := freePort()
	if err != nil {
		return "", fmt.Errorf("slm: find free port: %w", err)
	}

	// Invoke via sh to bypass Wine binfmt_misc interception of APE polyglot binaries.
	// llamafile is a valid POSIX shell script; sh executes the embedded launcher header.
	cmd := exec.CommandContext(ctx, "sh", mf.FilePath,
		"--server",
		"--host", "127.0.0.1",
		"--port", strconv.Itoa(port),
		"--nobrowser",
	)
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("slm: start llamafile: %w", err)
	}

	m.process = cmd.Process
	m.port = port

	if err := os.WriteFile(m.pidPath(), []byte(strconv.Itoa(cmd.Process.Pid)), 0600); err != nil {
		m.logger.Warn("failed to write pid file", "error", err)
	}

	baseURL := fmt.Sprintf("http://127.0.0.1:%d", port)
	m.logger.Info("llamafile started", "pid", cmd.Process.Pid, "url", baseURL)

	if err := waitHealthy(ctx, baseURL); err != nil {
		_ = m.Stop()
		return "", err
	}

	return baseURL, nil
}

// Stop terminates the llamafile process and cleans up the PID file.
func (m *Manager) Stop() error {
	if m.process == nil {
		return nil
	}
	if err := m.process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("slm: kill llamafile: %w", err)
	}
	m.process = nil
	m.port = 0
	_ = os.Remove(m.pidPath())
	return nil
}

// BaseURL returns the current server base URL, or "" if not running.
func (m *Manager) BaseURL() string {
	if m.process == nil || m.port == 0 {
		return ""
	}
	return fmt.Sprintf("http://127.0.0.1:%d", m.port)
}

// Manifest returns the on-disk manifest if present, or nil.
func (m *Manager) Manifest() *Manifest {
	mf, err := readManifest(m.cfg.DataDir)
	if err != nil {
		return nil
	}
	return mf
}

func (m *Manager) pidPath() string {
	return filepath.Join(m.cfg.DataDir, pidFile)
}

func (m *Manager) reapStalePID() {
	data, err := os.ReadFile(m.pidPath())
	if err != nil {
		return
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		_ = os.Remove(m.pidPath())
		return
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		_ = os.Remove(m.pidPath())
		return
	}
	_ = proc.Kill()
	_ = os.Remove(m.pidPath())
	m.logger.Debug("reaped stale llamafile process", "pid", pid)
}

// freePort binds on :0 to let the OS pick an available port, then releases it.
// There is a small TOCTOU window between release and use, which is acceptable for a local dev tool.
func freePort() (int, error) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port, nil
}

// waitHealthy polls baseURL/health until it returns 200 or ctx is cancelled.
// Ceiling: 15 seconds (cold model load can take 5–10 s).
func waitHealthy(ctx context.Context, baseURL string) error {
	deadline := time.Now().Add(15 * time.Second)
	client := &http.Client{Timeout: 2 * time.Second}

	for time.Now().Before(deadline) {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		resp, err := client.Get(baseURL + "/health")
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}

		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("slm: health check timed out after 15s")
}

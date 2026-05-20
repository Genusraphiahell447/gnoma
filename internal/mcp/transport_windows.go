//go:build windows

package mcp

import (
	"os"
	"os/exec"
)

// setProcessGroup is a no-op on Windows; we rely on os.Process.Kill in Close.
// Pulling in golang.org/x/sys/windows + job objects to kill the full process
// tree would be the upgrade path if MCP servers ever spawn children we need
// to reap.
func setProcessGroup(cmd *exec.Cmd) {}

func killProcessTree(p *os.Process) error {
	return p.Kill()
}

//go:build !windows

package mcp

import (
	"os"
	"os/exec"
	"syscall"
)

func setProcessGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killProcessTree(p *os.Process) error {
	// Negative PID targets the process group created via Setpgid.
	return syscall.Kill(-p.Pid, syscall.SIGKILL)
}

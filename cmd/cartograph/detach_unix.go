//go:build unix

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// detach starts cmd in a session of its own, so it outlives this one.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

// stop asks a server started by detach to shut down cleanly.
func stop(p *os.Process) error {
	return p.Signal(syscall.SIGTERM)
}

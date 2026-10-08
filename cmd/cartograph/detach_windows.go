//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// detach starts cmd in a process group of its own, so a console's
// interrupt does not reach it and it outlives this command.
func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// stop ends a server started by detach. Windows has no SIGTERM to send
// another process, so it is killed.
func stop(p *os.Process) error {
	return p.Kill()
}

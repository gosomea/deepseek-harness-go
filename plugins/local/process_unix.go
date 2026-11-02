//go:build !windows

package local

import (
	"os"
	"os/exec"
	"syscall"
)

// processGroupAttr puts the child in its own process group.
//
// Without a separate group, signalling on cancellation would reach this process
// and everything else it started.
func processGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{Setpgid: true}
}

// killProcessTree terminates the child's whole process group.
//
// A negative pid addresses the group. Killing the group rather than the process
// is what stops a shell script's children from outliving the call that started
// them.
func killProcessTree(process *os.Process) error {
	if process == nil {
		return nil
	}
	// Signal the group, then the process itself in case it never joined one.
	if err := syscall.Kill(-process.Pid, syscall.SIGKILL); err != nil {
		return process.Kill()
	}
	return nil
}

var _ = exec.Command

//go:build windows

package local

import (
	"os"
	"os/exec"
	"syscall"
)

// processGroupAttr creates a new process group on Windows.
//
// Windows has no pgid to signal, so tree termination uses taskkill below; the
// flag still isolates the child from this process's console signals.
func processGroupAttr() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP}
}

// killProcessTree terminates the child and its descendants.
//
// Windows has no process-group signal, so the whole tree is killed by pid. This
// path is written but not executed on this machine; the platform matrix records
// it as unverified rather than as working.
func killProcessTree(process *os.Process) error {
	if process == nil {
		return nil
	}
	kill := exec.Command("taskkill", "/T", "/F", "/PID", itoa(process.Pid))
	if err := kill.Run(); err != nil {
		return process.Kill()
	}
	return nil
}

// itoa renders a pid without importing strconv in a platform file.
func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits [20]byte
	index := len(digits)
	for value > 0 {
		index--
		digits[index] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		index--
		digits[index] = '-'
	}
	return string(digits[index:])
}

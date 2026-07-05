//go:build !windows

package runstate

import (
	"os"
	"syscall"
)

// pidAlive reports whether a process with this pid exists. Signal 0
// performs the existence check without delivering anything; EPERM still
// means the process is there.
func pidAlive(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil || err == syscall.EPERM
}

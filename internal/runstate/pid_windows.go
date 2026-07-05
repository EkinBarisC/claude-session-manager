//go:build windows

package runstate

import (
	"golang.org/x/sys/windows"
)

// pidAlive reports whether a process with this pid exists. Opening the
// handle with minimal rights avoids false negatives from access denial.
func pidAlive(pid int) bool {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return true // handle opened; assume alive
	}
	return code == 259 // STILL_ACTIVE
}

//go:build windows

package schedule

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// Install registers a daily Task Scheduler job that wakes the PC.
func Install(startHHMM, untilHHMM string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	exe = strings.ReplaceAll(exe, "'", "''")
	ps := fmt.Sprintf(
		"$action = New-ScheduledTaskAction -Execute '%s' -Argument 'run --until %s'; "+
			"$trigger = New-ScheduledTaskTrigger -Daily -At %s; "+
			"$settings = New-ScheduledTaskSettingsSet -WakeToRun -StartWhenAvailable "+
			"-ExecutionTimeLimit (New-TimeSpan -Hours 10); "+
			"Register-ScheduledTask -TaskName '%s' -Action $action "+
			"-Trigger $trigger -Settings $settings -Force | Out-Null; "+
			"Write-Output 'Registered task %s: daily at %s, runs until %s, wakes the PC.'",
		exe, untilHHMM, startHHMM, TaskName, TaskName, startHHMM, untilHHMM)
	return powershell(ps)
}

func Remove() error {
	ps := fmt.Sprintf(
		"Unregister-ScheduledTask -TaskName '%s' -Confirm:$false; "+
			"Write-Output 'Removed task %s.'", TaskName, TaskName)
	return powershell(ps)
}

func Exists() bool {
	cmd := exec.Command("schtasks", "/Query", "/TN", TaskName)
	return cmd.Run() == nil
}

func powershell(command string) error {
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", command)
	out, err := cmd.CombinedOutput()
	if text := strings.TrimSpace(string(out)); text != "" {
		fmt.Println(strings.SplitN(text, "\r\n", 2)[0])
	}
	if err != nil {
		return fmt.Errorf("scheduler error: %w", err)
	}
	return nil
}

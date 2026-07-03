"""Windows Task Scheduler integration (nightly quiet-hours run, wake-to-run)."""

import subprocess
import sys

TASK_NAME = "csm-nightly"


def install(start_hhmm: str, until_hhmm: str) -> int:
    python = sys.executable.replace("'", "''")
    argument = f"-m csm run --until {until_hhmm}"
    ps = (
        f"$action = New-ScheduledTaskAction -Execute '{python}' -Argument '{argument}'; "
        f"$trigger = New-ScheduledTaskTrigger -Daily -At {start_hhmm}; "
        "$settings = New-ScheduledTaskSettingsSet -WakeToRun -StartWhenAvailable "
        "-ExecutionTimeLimit (New-TimeSpan -Hours 10); "
        f"Register-ScheduledTask -TaskName '{TASK_NAME}' -Action $action "
        "-Trigger $trigger -Settings $settings -Force | Out-Null; "
        f"Write-Output 'Registered task {TASK_NAME}: daily at {start_hhmm}, runs until {until_hhmm}, wakes the PC.'"
    )
    return _powershell(ps)


def remove() -> int:
    ps = (
        f"Unregister-ScheduledTask -TaskName '{TASK_NAME}' -Confirm:$false; "
        f"Write-Output 'Removed task {TASK_NAME}.'"
    )
    return _powershell(ps)


def _powershell(command: str) -> int:
    proc = subprocess.run(
        ["powershell", "-NoProfile", "-NonInteractive", "-Command", command],
        capture_output=True, text=True,
    )
    out = (proc.stdout or "").strip()
    err = (proc.stderr or "").strip()
    if out:
        print(out)
    if proc.returncode != 0 and err:
        print(f"csm: scheduler error: {err.splitlines()[0]}")
    return proc.returncode

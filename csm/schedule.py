"""Nightly job registration: Windows Task Scheduler, cron on macOS/Linux."""

import os
import shlex
import shutil
import subprocess
import sys

from . import config

TASK_NAME = "csm-nightly"
CRON_MARKER = "# csm-nightly"

IS_WINDOWS = sys.platform == "win32"


def install(start_hhmm: str, until_hhmm: str) -> int:
    if IS_WINDOWS:
        return _install_windows(start_hhmm, until_hhmm)
    return _install_cron(start_hhmm, until_hhmm)


def remove() -> int:
    if IS_WINDOWS:
        return _remove_windows()
    return _remove_cron()


def exists() -> bool:
    if IS_WINDOWS:
        proc = subprocess.run(
            ["schtasks", "/Query", "/TN", TASK_NAME],
            capture_output=True, text=True,
        )
        return proc.returncode == 0
    return CRON_MARKER in _read_crontab()


# --- Windows Task Scheduler ---

def _install_windows(start_hhmm: str, until_hhmm: str) -> int:
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


def _remove_windows() -> int:
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


# --- cron (macOS / Linux) ---

def _install_cron(start_hhmm: str, until_hhmm: str) -> int:
    if not shutil.which("crontab"):
        print("csm: 'crontab' not found - install cron or schedule "
              f"`{sys.executable} -m csm run --until {until_hhmm}` yourself")
        return 1
    hour, minute = start_hhmm.split(":")
    log = config.state_dir() / "cron.log"
    # cron runs with a minimal PATH; carry the current one so the claude
    # binary resolves the same way it does in an interactive shell.
    command = (
        f"PATH={shlex.quote(os.environ.get('PATH', ''))} "
        f"{shlex.quote(sys.executable)} -m csm run --until {until_hhmm} "
        f">> {shlex.quote(str(log))} 2>&1"
    )
    # % is special in crontab lines (command terminator / stdin marker)
    command = command.replace("%", r"\%")
    line = f"{int(minute)} {int(hour)} * * * {command} {CRON_MARKER}"
    kept = [l for l in _read_crontab().splitlines() if CRON_MARKER not in l]
    rc = _write_crontab("\n".join(kept + [line]) + "\n")
    if rc == 0:
        print(f"Registered cron entry {CRON_MARKER}: daily at {start_hhmm}, "
              f"runs until {until_hhmm}. Output: {log}")
        print("csm: note - cron does not wake a sleeping machine; keep it awake "
              "or set a wake alarm (`pmset repeat wakeorpoweron` on macOS, "
              "`rtcwake` on Linux).")
    return rc


def _remove_cron() -> int:
    current = _read_crontab()
    if CRON_MARKER not in current:
        print(f"csm: no {CRON_MARKER} cron entry found")
        return 0
    kept = [l for l in current.splitlines() if CRON_MARKER not in l]
    rc = _write_crontab("\n".join(kept) + "\n" if kept else "")
    if rc == 0:
        print(f"Removed cron entry {CRON_MARKER}.")
    return rc


def _read_crontab() -> str:
    if not shutil.which("crontab"):
        return ""
    proc = subprocess.run(["crontab", "-l"], capture_output=True, text=True)
    return proc.stdout if proc.returncode == 0 else ""


def _write_crontab(content: str) -> int:
    proc = subprocess.run(["crontab", "-"], input=content,
                          capture_output=True, text=True)
    if proc.returncode != 0:
        err = (proc.stderr or "crontab failed").strip().splitlines()[0]
        print(f"csm: scheduler error: {err}")
    return proc.returncode

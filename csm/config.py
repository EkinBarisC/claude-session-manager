"""Config and state paths. All state lives in ~/.csm (override with CSM_HOME)."""

import json
import os
import re
from pathlib import Path

EFFORT_LEVELS = ("low", "medium", "high", "xhigh", "max")
RUN_MODES = ("plan", "safe", "full")

_HHMM_RE = re.compile(r"^([01]?\d|2[0-3]):[0-5]\d$")


def state_dir() -> Path:
    return Path(os.environ.get("CSM_HOME", str(Path.home() / ".csm")))


def config_path() -> Path:
    return state_dir() / "config.json"


def queue_path() -> Path:
    return state_dir() / "queue.json"


def sessions_path() -> Path:
    return state_dir() / "sessions.json"


def ledger_path() -> Path:
    return state_dir() / "ledger.json"


def report_path() -> Path:
    return state_dir() / "report.md"


DEFAULTS = {
    # Model for queue items that don't specify one. Pro has no Opus in
    # Claude Code; sonnet stretches each 5h window across more items.
    "default_model": "sonnet",
    # Effort passed to `claude --effort` (low|medium|high|xhigh|max).
    # Claude Code's own default is xhigh; medium stretches quota further.
    # Set to null to use the CLI default.
    "default_effort": "medium",
    # Run mode for items without --mode:
    #   plan  -> read-only planning (--permission-mode plan)
    #   safe  -> allowlisted edits/tests/commits, push blocked (default)
    #   full  -> --dangerously-skip-permissions (use only for sandboxed dirs)
    "default_run_mode": "safe",
    # Rolling 7-day budget of weighted tokens the bot may spend
    # (input + cache_creation + output + 0.1 * cache_read).
    "weekly_token_budget": 1_000_000,
    # Context rotation: when a session's estimated context exceeds
    # context_rotate_pct % of context_window_tokens, the next task for that
    # project starts a fresh session (which reads context.md).
    "context_window_tokens": 200_000,
    "context_rotate_pct": 40,
    # Hard timeout per queue item.
    "item_timeout_minutes": 30,
    # Used by `csm schedule` for the nightly Task Scheduler job.
    "quiet_hours_start": "00:30",
    "quiet_hours_end": "07:30",
    "claude_binary": "claude",
    # Passed to `claude -p` via --allowedTools / --disallowedTools.
    # Edits, tests, and branch-local git are allowed; push and destructive
    # operations are blocked.
    "allowed_tools": [
        "Read",
        "Edit",
        "Write",
        "Glob",
        "Grep",
        "WebSearch",
        "WebFetch",
        "Bash(git status:*)",
        "Bash(git diff:*)",
        "Bash(git log:*)",
        "Bash(git add:*)",
        "Bash(git commit:*)",
        "Bash(git branch:*)",
        "Bash(git checkout:*)",
        "Bash(git switch:*)",
        "Bash(git stash:*)",
        "Bash(mkdir:*)",
        "Bash(npm test:*)",
        "Bash(npm run:*)",
        "Bash(npx:*)",
        "Bash(node:*)",
        "Bash(python:*)",
        "Bash(py:*)",
        "Bash(pytest:*)",
        "Bash(pip install:*)",
    ],
    "disallowed_tools": [
        "Bash(git push:*)",
        "Bash(git reset:*)",
        "Bash(git rebase:*)",
        "Bash(git clean:*)",
        "Bash(git remote:*)",
        "Bash(rm:*)",
        "Bash(del:*)",
        "Bash(rmdir:*)",
    ],
}


def load() -> dict:
    cfg = dict(DEFAULTS)
    path = config_path()
    if path.exists():
        try:
            cfg.update(json.loads(path.read_text(encoding="utf-8")))
        except (json.JSONDecodeError, OSError) as exc:
            raise SystemExit(f"csm: cannot read {path}: {exc}")
    return cfg


def ensure_init() -> Path:
    d = state_dir()
    d.mkdir(parents=True, exist_ok=True)
    path = config_path()
    if not path.exists():
        path.write_text(json.dumps(DEFAULTS, indent=2) + "\n", encoding="utf-8")
    return path


def overrides() -> dict:
    """The raw config file contents (no defaults merged in)."""
    return read_json(config_path(), {})


def parse_value(raw: str):
    """CLI value -> typed value: JSON if it parses, plain string otherwise."""
    try:
        return json.loads(raw)
    except json.JSONDecodeError:
        return raw


def validate(key: str, value) -> str | None:
    """Error message if (key, value) is not a valid setting, else None."""
    if key not in DEFAULTS:
        return f"unknown key '{key}' (known: {', '.join(sorted(DEFAULTS))})"
    if key == "default_effort":
        if value is not None and value not in EFFORT_LEVELS:
            return f"default_effort must be one of {', '.join(EFFORT_LEVELS)} or null"
    elif key == "default_run_mode":
        if value not in RUN_MODES:
            return f"default_run_mode must be one of {', '.join(RUN_MODES)}"
    elif key in ("weekly_token_budget", "context_window_tokens", "item_timeout_minutes"):
        if not isinstance(value, int) or isinstance(value, bool) or value <= 0:
            return f"{key} must be a positive integer"
    elif key == "context_rotate_pct":
        if not isinstance(value, int) or isinstance(value, bool) or not 1 <= value <= 100:
            return "context_rotate_pct must be an integer between 1 and 100"
    elif key in ("quiet_hours_start", "quiet_hours_end"):
        if not isinstance(value, str) or not _HHMM_RE.match(value):
            return f"{key} must be HH:MM (24h), e.g. 00:30"
    elif key in ("allowed_tools", "disallowed_tools"):
        if not isinstance(value, list) or not all(isinstance(v, str) for v in value):
            return f'{key} must be a JSON list of strings, e.g. \'["Read", "Edit"]\''
    elif key in ("default_model", "claude_binary"):
        if not isinstance(value, str) or not value.strip():
            return f"{key} must be a non-empty string"
    return None


def set_value(key: str, value) -> None:
    ensure_init()
    data = overrides()
    data[key] = value
    write_json(config_path(), data)


def unset_value(key: str) -> bool:
    """Reset a key to its default. Returns True if it was overridden."""
    ensure_init()
    data = overrides()
    if key not in data:
        return False
    del data[key]
    write_json(config_path(), data)
    return True


def read_json(path: Path, default):
    if not path.exists():
        return default
    try:
        return json.loads(path.read_text(encoding="utf-8"))
    except (json.JSONDecodeError, OSError):
        return default


def write_json(path: Path, data) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(path.suffix + ".tmp")
    tmp.write_text(json.dumps(data, indent=2) + "\n", encoding="utf-8")
    tmp.replace(path)

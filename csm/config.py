"""Config and state paths. All state lives in ~/.csm (override with CSM_HOME)."""

import json
import os
from pathlib import Path


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

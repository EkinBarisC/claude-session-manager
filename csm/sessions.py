"""Per-project session registry: ~/.csm/sessions.json.

Maps a project directory to its most recent Claude Code session and an
estimate of that session's context size, so the runner can decide between
resuming (`claude -r`) and rotating to a fresh session.
"""

from datetime import datetime, timezone
from pathlib import Path

from . import config


def load() -> dict:
    return config.read_json(config.sessions_path(), {})


def save(registry: dict) -> None:
    config.write_json(config.sessions_path(), registry)


def _key(project: str) -> str:
    return str(Path(project).resolve()).lower()


def resumable_session(registry: dict, cfg: dict, project: str) -> str | None:
    """Session id to resume, or None if a fresh session should be started."""
    entry = registry.get(_key(project))
    if not entry or not entry.get("session_id"):
        return None
    threshold = cfg["context_window_tokens"] * cfg["context_rotate_pct"] / 100
    if entry.get("context_tokens", 0) >= threshold:
        return None  # rotate: context.md carries the state forward
    return entry["session_id"]


def record(registry: dict, project: str, session_id: str, context_tokens: int) -> None:
    registry[_key(project)] = {
        "session_id": session_id,
        "context_tokens": context_tokens,
        "updated_at": datetime.now(timezone.utc).isoformat(timespec="seconds"),
    }
    save(registry)

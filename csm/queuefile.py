"""Queue persistence: ~/.csm/queue.json.

Item statuses:
  pending          waiting to run
  done             completed successfully
  needs_attention  failed, timed out, or ended asking a question
"""

import secrets
from datetime import datetime, timezone
from pathlib import Path

from . import config

PENDING = "pending"
DONE = "done"
NEEDS_ATTENTION = "needs_attention"


def _now() -> str:
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def load_items() -> list:
    return config.read_json(config.queue_path(), [])


def save_items(items: list) -> None:
    config.write_json(config.queue_path(), items)


def add_item(prompt: str, project: str, model: str | None, priority: int,
             force_new_session: bool, effort: str | None = None,
             mode: str | None = None) -> dict:
    items = load_items()
    item = {
        "id": secrets.token_hex(4),
        "prompt": prompt,
        "project": str(Path(project).resolve()),
        "model": model,
        "effort": effort,
        "mode": mode,
        "priority": priority,
        "force_new_session": force_new_session,
        "status": PENDING,
        "created_at": _now(),
        "session_id": None,
        "summary": None,
        "error": None,
        "tokens": None,
        "finished_at": None,
    }
    items.append(item)
    save_items(items)
    return item


def find_item(items: list, token: str) -> tuple[dict | None, str | None]:
    """Look up an item by id or unique id prefix. Returns (item, error)."""
    matches = [i for i in items if i["id"] == token]
    if not matches:
        matches = [i for i in items if i["id"].startswith(token)]
    if len(matches) == 1:
        return matches[0], None
    if not matches:
        return None, f"no item matching '{token}'"
    return None, (f"'{token}' is ambiguous: "
                  + ", ".join(i["id"] for i in matches))


def pending_items(items: list) -> list:
    todo = [i for i in items if i["status"] == PENDING]
    todo.sort(key=lambda i: (-i.get("priority", 0), i["created_at"]))
    return todo


def finish_item(items: list, item: dict, status: str, *, session_id=None,
                summary=None, error=None, tokens=None) -> None:
    item["status"] = status
    item["session_id"] = session_id or item.get("session_id")
    item["summary"] = summary
    item["error"] = error
    item["tokens"] = tokens
    item["finished_at"] = _now()
    save_items(items)

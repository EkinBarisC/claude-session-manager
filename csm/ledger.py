"""Usage ledger: ~/.csm/ledger.json.

Every run appends a token record. The weekly budget guard works on a
rolling 7-day sum of *weighted* tokens:

    input + cache_creation + output + 0.1 * cache_read

This is a heuristic for subscription quota pressure, not billing (nothing
is billed - subscription auth only). Tune weekly_token_budget in config
after observing a normal week in `csm status`.
"""

from datetime import datetime, timedelta, timezone

from . import config


def load() -> list:
    return config.read_json(config.ledger_path(), [])


def weighted(usage: dict) -> int:
    return round(
        usage.get("input_tokens", 0)
        + usage.get("cache_creation_input_tokens", 0)
        + usage.get("output_tokens", 0)
        + 0.1 * usage.get("cache_read_input_tokens", 0)
    )


def append(item_id: str, project: str, model: str, usage: dict) -> None:
    records = load()
    records.append({
        "ts": datetime.now(timezone.utc).isoformat(timespec="seconds"),
        "item_id": item_id,
        "project": project,
        "model": model,
        "usage": usage,
        "weighted": weighted(usage),
    })
    config.write_json(config.ledger_path(), records)


def weekly_spend(records: list | None = None) -> int:
    if records is None:
        records = load()
    cutoff = datetime.now(timezone.utc) - timedelta(days=7)
    total = 0
    for rec in records:
        try:
            ts = datetime.fromisoformat(rec["ts"])
        except (KeyError, ValueError):
            continue
        if ts >= cutoff:
            total += rec.get("weighted", 0)
    return total

"""Appends the per-item run report: ~/.csm/report.md."""

from datetime import datetime

from . import config


def append_run_header(trigger: str) -> None:
    _append(f"\n## Run {datetime.now():%Y-%m-%d %H:%M} ({trigger})\n")


def append_item(item: dict, status: str, session_id: str | None,
                summary: str | None, error: str | None, weighted_tokens: int,
                weekly_spend: int, budget: int) -> None:
    lines = [
        f"### [{status}] {item['id']} - {_short(item['prompt'])}",
        f"- project: `{item['project']}`",
    ]
    if session_id:
        lines.append(f"- session: `{session_id}` (resume with `claude -r {session_id}`)")
    if summary:
        lines.append(f"- summary: {summary}")
    if error:
        lines.append(f"- error: {error}")
    lines.append(f"- tokens (weighted): {weighted_tokens:,} | week: {weekly_spend:,} / {budget:,}")
    _append("\n".join(lines) + "\n")


def append_note(text: str) -> None:
    _append(f"- {text}\n")


def _append(text: str) -> None:
    path = config.report_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("a", encoding="utf-8") as fh:
        fh.write(text)


def _short(prompt: str, limit: int = 80) -> str:
    line = prompt.strip().splitlines()[0] if prompt.strip() else ""
    return line[:limit] + ("..." if len(line) > limit else "")

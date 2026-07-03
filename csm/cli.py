"""csm command-line interface."""

import argparse
import json
from pathlib import Path

from . import __version__, config, ledger, queuefile, runner, schedule, sessions


def main(argv=None) -> int:
    parser = argparse.ArgumentParser(
        prog="csm",
        description="Claude Session Manager - spend leftover Claude Pro quota "
                    "via Claude Code headless runs (subscription auth only).",
    )
    parser.add_argument("--version", action="version", version=f"csm {__version__}")
    sub = parser.add_subparsers(dest="command", required=True)

    sub.add_parser("init", help="create ~/.csm with default config")

    p_add = sub.add_parser("add", help="queue a task")
    p_add.add_argument("prompt", help="the task prompt")
    p_add.add_argument("--project", required=True, help="project directory the task runs in")
    p_add.add_argument("--model", default=None, help="model override (default: config default_model)")
    p_add.add_argument("--priority", type=int, default=0, help="higher runs first (default 0)")
    p_add.add_argument("--new-session", action="store_true",
                       help="force a fresh session even if one is resumable")

    sub.add_parser("status", help="queue overview, weekly spend, session registry")

    p_run = sub.add_parser("run", help="run pending items now (manual burn or scheduled)")
    p_run.add_argument("--until", metavar="HH:MM", default=None,
                       help="stop starting new items at this local time")
    p_run.add_argument("--max-items", type=int, default=None)
    p_run.add_argument("--dry-run", action="store_true",
                       help="show what would run without invoking claude")

    p_report = sub.add_parser("report", help="show the run report")
    p_report.add_argument("--tail", type=int, default=60, help="lines to show (default 60)")

    p_sched = sub.add_parser("schedule", help="register/remove the nightly Task Scheduler job")
    p_sched.add_argument("--start", metavar="HH:MM", default=None,
                         help="nightly start (default: config quiet_hours_start)")
    p_sched.add_argument("--until", metavar="HH:MM", default=None,
                         help="nightly end (default: config quiet_hours_end)")
    p_sched.add_argument("--remove", action="store_true", help="remove the scheduled task")

    sub.add_parser("config", help="print config path and contents")

    p_requeue = sub.add_parser("requeue", help="set an item back to pending")
    p_requeue.add_argument("item_id")

    args = parser.parse_args(argv)
    return _dispatch(args)


def _dispatch(args) -> int:
    if args.command == "init":
        path = config.ensure_init()
        print(f"csm: initialized. Config: {path}")
        print("csm: make sure Claude Code is logged in with your Pro account "
              "(run `claude` once and use /login).")
        return 0

    if args.command == "add":
        project = Path(args.project)
        if not project.is_dir():
            print(f"csm: project directory not found: {project}")
            return 1
        if args.model and "opus" in args.model.lower():
            print("csm: warning - Pro accounts have no Opus access in Claude Code; "
                  "this item will likely fail. Queued anyway.")
        config.ensure_init()
        item = queuefile.add_item(args.prompt, str(project), args.model,
                                  args.priority, args.new_session)
        print(f"csm: queued [{item['id']}] for {item['project']}")
        return 0

    if args.command == "status":
        return _status()

    if args.command == "run":
        return runner.run(args.until, args.max_items, args.dry_run)

    if args.command == "report":
        path = config.report_path()
        if not path.exists():
            print("csm: no report yet")
            return 0
        lines = path.read_text(encoding="utf-8").splitlines()
        print(f"--- {path} (last {min(args.tail, len(lines))} lines) ---")
        print("\n".join(lines[-args.tail:]))
        return 0

    if args.command == "schedule":
        if args.remove:
            return schedule.remove()
        cfg = config.load()
        return schedule.install(args.start or cfg["quiet_hours_start"],
                                args.until or cfg["quiet_hours_end"])

    if args.command == "config":
        config.ensure_init()
        print(f"# {config.config_path()}")
        print(json.dumps(config.load(), indent=2))
        return 0

    if args.command == "requeue":
        items = queuefile.load_items()
        for item in items:
            if item["id"] == args.item_id:
                item["status"] = queuefile.PENDING
                item["error"] = None
                queuefile.save_items(items)
                print(f"csm: [{item['id']}] back to pending")
                return 0
        print(f"csm: no item with id {args.item_id}")
        return 1

    return 1


def _status() -> int:
    items = queuefile.load_items()
    cfg = config.load()
    by_status = {}
    for item in items:
        by_status.setdefault(item["status"], []).append(item)

    print(f"Queue ({len(items)} items): "
          + ", ".join(f"{k}={len(v)}" for k, v in sorted(by_status.items()))
          if items else "Queue: empty")
    for item in queuefile.pending_items(items):
        model = item.get("model") or cfg["default_model"]
        print(f"  [{item['id']}] p{item.get('priority', 0)} {model} "
              f"{item['project']} :: {item['prompt'][:60]}")
    for item in by_status.get(queuefile.NEEDS_ATTENTION, []):
        print(f"  [{item['id']}] NEEDS ATTENTION: {item.get('error') or ''} "
              + (f"(claude -r {item['session_id']})" if item.get("session_id") else ""))

    spend = ledger.weekly_spend()
    budget = cfg["weekly_token_budget"]
    pct = 100 * spend / budget if budget else 0
    print(f"Weekly spend: {spend:,} / {budget:,} weighted tokens ({pct:.0f}%)")

    registry = sessions.load()
    if registry:
        threshold = cfg["context_window_tokens"] * cfg["context_rotate_pct"] / 100
        print("Sessions:")
        for project, entry in registry.items():
            ctx = entry.get("context_tokens", 0)
            state = "will rotate" if ctx >= threshold else "resumable"
            print(f"  {project}: {ctx:,} ctx tokens ({state})")
    return 0

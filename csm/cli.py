"""csm command-line interface."""

import argparse
import json
import os
import shutil
import subprocess
from pathlib import Path

from . import (__version__, claude_runner, config, ledger, queuefile, runner,
               schedule, sessions)

EXAMPLES = """\
examples:
  csm init                                    set up ~/.csm
  csm add "Fix the failing tests in src/"     queue a task in the current directory
  csm add "Refactor auth" -C C:\\code\\app --effort high --mode plan
  csm list                                    pending and failed items
  csm run --max-items 1                       run one item now
  csm run --until 08:00                       run until 08:00
  csm config set default_effort low           change a setting
  csm requeue --all                           retry everything that failed
  csm doctor                                  check the installation

Run `csm <command> -h` for details on any command.
"""

STATUSES = (queuefile.PENDING, queuefile.DONE, queuefile.NEEDS_ATTENTION)


def build_parser() -> argparse.ArgumentParser:
    parser = argparse.ArgumentParser(
        prog="csm",
        description="Claude Session Manager - spend leftover Claude Pro quota "
                    "via Claude Code headless runs (subscription auth only).",
        epilog=EXAMPLES,
        formatter_class=argparse.RawDescriptionHelpFormatter,
    )
    parser.add_argument("--version", action="version", version=f"csm {__version__}")
    sub = parser.add_subparsers(dest="command", required=True, metavar="<command>")

    sub.add_parser("init", help="create ~/.csm with default config")

    p_add = sub.add_parser("add", help="queue a task")
    p_add.add_argument("prompt", help="the task prompt")
    p_add.add_argument("--project", "-C", default=".",
                       help="project directory the task runs in (default: current directory)")
    p_add.add_argument("--model", "-m", default=None,
                       help="model override (default: config default_model)")
    p_add.add_argument("--effort", "-e", choices=config.EFFORT_LEVELS, default=None,
                       help="reasoning effort (default: config default_effort)")
    p_add.add_argument("--mode", choices=config.RUN_MODES, default=None,
                       help="plan = read-only planning, safe = allowlisted edits (default), "
                            "full = skip all permission checks")
    p_add.add_argument("--priority", "-p", type=int, default=0,
                       help="higher runs first (default 0)")
    p_add.add_argument("--new-session", action="store_true",
                       help="force a fresh session even if one is resumable")

    p_list = sub.add_parser("list", aliases=["ls"], help="list queue items")
    p_list.add_argument("--status", choices=STATUSES,
                        help="only items with this status")
    p_list.add_argument("--all", "-a", action="store_true",
                        help="include done items (default: pending + needs_attention)")

    p_show = sub.add_parser("show", help="show one item in full")
    p_show.add_argument("item_id", help="item id (or unique prefix)")

    p_edit = sub.add_parser("edit", help="change a queued item")
    p_edit.add_argument("item_id", help="item id (or unique prefix)")
    p_edit.add_argument("--prompt", default=None)
    p_edit.add_argument("--model", "-m", default=None)
    p_edit.add_argument("--effort", "-e", choices=config.EFFORT_LEVELS, default=None)
    p_edit.add_argument("--mode", choices=config.RUN_MODES, default=None)
    p_edit.add_argument("--priority", "-p", type=int, default=None)

    p_rm = sub.add_parser("rm", aliases=["remove"], help="delete items from the queue")
    p_rm.add_argument("item_ids", nargs="+", metavar="item_id",
                      help="item ids (or unique prefixes)")

    p_clear = sub.add_parser("clear", help="remove done items from the queue")
    p_clear.add_argument("--all", action="store_true",
                         help="remove every item regardless of status")

    p_requeue = sub.add_parser("requeue", help="set items back to pending")
    p_requeue.add_argument("item_ids", nargs="*", metavar="item_id",
                           help="item ids (or unique prefixes)")
    p_requeue.add_argument("--all", action="store_true",
                           help="requeue every needs_attention item")
    p_requeue.add_argument("--new-session", action="store_true",
                           help="also force a fresh session on the retry")

    sub.add_parser("status", help="queue overview, weekly spend, session registry")

    p_run = sub.add_parser("run", help="run pending items now (manual burn or scheduled)")
    p_run.add_argument("--until", metavar="HH:MM", default=None,
                       help="stop starting new items at this local time")
    p_run.add_argument("--max-items", type=int, default=None)
    p_run.add_argument("--id", dest="item_id", default=None,
                       help="run only this item (id or unique prefix)")
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

    p_cfg = sub.add_parser("config", help="show or change settings")
    cfg_sub = p_cfg.add_subparsers(dest="config_action", metavar="<action>")
    cfg_sub.add_parser("show", help="print the effective config (default)")
    p_get = cfg_sub.add_parser("get", help="print one value")
    p_get.add_argument("key")
    p_set = cfg_sub.add_parser("set", help="set a value (JSON or plain string)")
    p_set.add_argument("key")
    p_set.add_argument("value")
    p_unset = cfg_sub.add_parser("unset", help="reset a key to its default")
    p_unset.add_argument("key")
    cfg_sub.add_parser("path", help="print the config file path")
    cfg_sub.add_parser("edit", help="open the config file in your editor")

    sub.add_parser("doctor", help="check claude login, config, and the scheduled job")

    return parser


def main(argv=None) -> int:
    args = build_parser().parse_args(argv)
    command = {"ls": "list", "remove": "rm"}.get(args.command, args.command)
    return _dispatch(command, args)


def _dispatch(command: str, args) -> int:
    if command == "init":
        path = config.ensure_init()
        print(f"csm: initialized. Config: {path}")
        print("csm: make sure Claude Code is logged in with your Pro account "
              "(run `claude` once and use /login).")
        return 0

    if command == "add":
        return _add(args)
    if command == "list":
        return _list(args)
    if command == "show":
        return _show(args)
    if command == "edit":
        return _edit(args)
    if command == "rm":
        return _rm(args)
    if command == "clear":
        return _clear(args)
    if command == "requeue":
        return _requeue(args)
    if command == "status":
        return _status()
    if command == "run":
        return runner.run(args.until, args.max_items, args.dry_run, args.item_id)
    if command == "config":
        return _config(args)
    if command == "doctor":
        return _doctor()

    if command == "report":
        path = config.report_path()
        if not path.exists():
            print("csm: no report yet")
            return 0
        lines = path.read_text(encoding="utf-8").splitlines()
        print(f"--- {path} (last {min(args.tail, len(lines))} lines) ---")
        print("\n".join(lines[-args.tail:]))
        return 0

    if command == "schedule":
        if args.remove:
            return schedule.remove()
        cfg = config.load()
        return schedule.install(args.start or cfg["quiet_hours_start"],
                                args.until or cfg["quiet_hours_end"])

    return 1


def _add(args) -> int:
    project = Path(args.project)
    if not project.is_dir():
        print(f"csm: project directory not found: {project}")
        return 1
    if args.model and "opus" in args.model.lower():
        print("csm: warning - Pro accounts have no Opus access in Claude Code; "
              "this item will likely fail. Queued anyway.")
    if args.mode == "full":
        print("csm: warning - mode 'full' skips ALL permission checks in that "
              "project; use only for sandboxed/throwaway directories.")
    config.ensure_init()
    item = queuefile.add_item(args.prompt, str(project), args.model,
                              args.priority, args.new_session,
                              effort=args.effort, mode=args.mode)
    print(f"csm: queued [{item['id']}] for {item['project']}")
    return 0


def _item_row(item: dict, cfg: dict) -> str:
    model, effort, mode = claude_runner.item_settings(cfg, item)
    head = (f"  [{item['id']}] {item['status']:<15} p{item.get('priority', 0)} "
            f"{model}/{effort or 'default'}/{mode}")
    tail = item.get("summary") or item.get("error") or ""
    line = f"{head}  {item['project']} :: {_short(item['prompt'])}"
    if tail:
        line += f"\n      -> {_short(tail)}"
    return line


def _short(text: str, limit: int = 70) -> str:
    line = text.strip().splitlines()[0] if text.strip() else ""
    return line[:limit] + ("..." if len(line) > limit else "")


def _list(args) -> int:
    items = queuefile.load_items()
    cfg = config.load()
    if args.status:
        items = [i for i in items if i["status"] == args.status]
    elif not args.all:
        items = [i for i in items if i["status"] != queuefile.DONE]
    if not items:
        print("csm: nothing to list (try --all)")
        return 0
    items.sort(key=lambda i: (i["status"] != queuefile.PENDING,
                              -i.get("priority", 0), i["created_at"]))
    for item in items:
        print(_item_row(item, cfg))
    return 0


def _show(args) -> int:
    items = queuefile.load_items()
    item, err = queuefile.find_item(items, args.item_id)
    if err:
        print(f"csm: {err}")
        return 1
    cfg = config.load()
    model, effort, mode = claude_runner.item_settings(cfg, item)
    print(f"id:        {item['id']}")
    print(f"status:    {item['status']}")
    print(f"project:   {item['project']}")
    print(f"model:     {model}" + ("" if item.get("model") else " (config default)"))
    print(f"effort:    {effort or 'cli default'}"
          + ("" if item.get("effort") else " (config default)"))
    print(f"mode:      {mode}" + ("" if item.get("mode") else " (config default)"))
    print(f"priority:  {item.get('priority', 0)}")
    print(f"created:   {item['created_at']}")
    if item.get("finished_at"):
        print(f"finished:  {item['finished_at']}")
    if item.get("session_id"):
        print(f"session:   {item['session_id']}  (resume with `claude -r {item['session_id']}`)")
    if item.get("summary"):
        print(f"summary:   {item['summary']}")
    if item.get("error"):
        print(f"error:     {item['error']}")
    if item.get("tokens"):
        print(f"tokens:    {json.dumps(item['tokens'])}")
    print(f"prompt:\n{item['prompt']}")
    return 0


def _edit(args) -> int:
    items = queuefile.load_items()
    item, err = queuefile.find_item(items, args.item_id)
    if err:
        print(f"csm: {err}")
        return 1
    changes = {k: getattr(args, k) for k in
               ("prompt", "model", "effort", "mode", "priority")
               if getattr(args, k) is not None}
    if not changes:
        print("csm: nothing to change (pass --prompt/--model/--effort/--mode/--priority)")
        return 1
    item.update(changes)
    queuefile.save_items(items)
    print(f"csm: [{item['id']}] updated: " + ", ".join(f"{k}={v}" for k, v in changes.items()))
    if item["status"] != queuefile.PENDING:
        print(f"csm: note - item is {item['status']}; `csm requeue {item['id']}` to run it")
    return 0


def _rm(args) -> int:
    items = queuefile.load_items()
    targets = []
    for token in args.item_ids:
        item, err = queuefile.find_item(items, token)
        if err:
            print(f"csm: {err}")
            return 1
        targets.append(item)
    remaining = [i for i in items if i not in targets]
    queuefile.save_items(remaining)
    for item in targets:
        print(f"csm: removed [{item['id']}] {_short(item['prompt'])}")
    return 0


def _clear(args) -> int:
    items = queuefile.load_items()
    if args.all:
        kept, removed = [], items
    else:
        kept = [i for i in items if i["status"] != queuefile.DONE]
        removed = [i for i in items if i["status"] == queuefile.DONE]
    if not removed:
        print("csm: nothing to clear")
        return 0
    queuefile.save_items(kept)
    print(f"csm: cleared {len(removed)} item(s), {len(kept)} left")
    return 0


def _requeue(args) -> int:
    items = queuefile.load_items()
    if args.all:
        targets = [i for i in items if i["status"] == queuefile.NEEDS_ATTENTION]
        if not targets:
            print("csm: no needs_attention items")
            return 0
    elif args.item_ids:
        targets = []
        for token in args.item_ids:
            item, err = queuefile.find_item(items, token)
            if err:
                print(f"csm: {err}")
                return 1
            targets.append(item)
    else:
        print("csm: pass item ids or --all")
        return 1
    for item in targets:
        item["status"] = queuefile.PENDING
        item["error"] = None
        if args.new_session:
            item["force_new_session"] = True
        print(f"csm: [{item['id']}] back to pending")
    queuefile.save_items(items)
    return 0


def _config(args) -> int:
    action = getattr(args, "config_action", None) or "show"
    config.ensure_init()

    if action == "show":
        print(f"# {config.config_path()}")
        print(json.dumps(config.load(), indent=2))
        return 0

    if action == "path":
        print(config.config_path())
        return 0

    if action == "get":
        cfg = config.load()
        if args.key not in cfg:
            print(f"csm: unknown key '{args.key}'")
            return 1
        print(json.dumps(cfg[args.key], indent=2))
        return 0

    if action == "set":
        value = config.parse_value(args.value)
        err = config.validate(args.key, value)
        if err:
            print(f"csm: {err}")
            return 1
        config.set_value(args.key, value)
        print(f"csm: {args.key} = {json.dumps(value)}")
        return 0

    if action == "unset":
        if args.key not in config.DEFAULTS:
            print(f"csm: unknown key '{args.key}'")
            return 1
        config.unset_value(args.key)
        print(f"csm: {args.key} reset to default: {json.dumps(config.DEFAULTS[args.key])}")
        return 0

    if action == "edit":
        path = config.config_path()
        editor = os.environ.get("EDITOR")
        if editor:
            return subprocess.call([editor, str(path)])
        if os.name == "nt":
            os.startfile(path)  # Windows-only: opens with the associated app
            return 0
        return subprocess.call(["vi", str(path)])

    return 1


def _doctor() -> int:
    cfg = config.load()
    problems = 0

    def check(ok: bool, label: str, detail: str = "") -> None:
        nonlocal problems
        mark = "ok  " if ok else "FAIL"
        print(f"  [{mark}] {label}" + (f" - {detail}" if detail else ""))
        if not ok:
            problems += 1

    print(f"csm {__version__} doctor")
    print(f"  state dir: {config.state_dir()}")

    binary = shutil.which(cfg["claude_binary"])
    if binary:
        try:
            proc = subprocess.run([binary, "--version"], capture_output=True,
                                  text=True, timeout=30)
            version = (proc.stdout or proc.stderr or "").strip().splitlines()[0]
            check(proc.returncode == 0, f"claude binary: {binary}", version)
        except (OSError, subprocess.TimeoutExpired) as exc:
            check(False, f"claude binary: {binary}", f"failed to run: {exc}")
    else:
        check(False, f"claude binary '{cfg['claude_binary']}' not on PATH",
              "install Claude Code and log in with your Pro account")

    bad = [k for k in config.overrides() if k not in config.DEFAULTS]
    invalid = [f"{k}: {config.validate(k, cfg[k])}" for k in config.DEFAULTS
               if config.validate(k, cfg[k])]
    check(not bad and not invalid, "config valid",
          "; ".join((["unknown keys: " + ", ".join(bad)] if bad else []) + invalid))

    billing = [k for k in claude_runner.STRIP_ENV if os.environ.get(k)]
    check(True, "billing env vars",
          f"{', '.join(billing)} set in your shell - csm strips them from every run"
          if billing else "none set")

    check(True, f"scheduled task '{schedule.TASK_NAME}'",
          "registered" if schedule.exists() else "not registered (run `csm schedule`)")

    items = queuefile.load_items()
    counts = {}
    for item in items:
        counts[item["status"]] = counts.get(item["status"], 0) + 1
    check(True, "queue",
          ", ".join(f"{k}={v}" for k, v in sorted(counts.items())) if items else "empty")
    check(True, "weekly spend",
          f"{ledger.weekly_spend():,} / {cfg['weekly_token_budget']:,} weighted tokens")

    print("csm: all checks passed" if problems == 0
          else f"csm: {problems} problem(s) found")
    return 0 if problems == 0 else 1


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
        model, effort, mode = claude_runner.item_settings(cfg, item)
        print(f"  [{item['id']}] p{item.get('priority', 0)} {model}/{effort or 'default'}/{mode} "
              f"{item['project']} :: {_short(item['prompt'], 60)}")
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

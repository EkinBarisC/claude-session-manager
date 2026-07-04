"""Orchestration loop: pick pending items, run them, park on limits."""

import os
import time
from datetime import datetime, timedelta

from . import claude_runner, config, ledger, queuefile, report, sessions


def resolve_until(until_hhmm: str | None) -> datetime | None:
    if not until_hhmm:
        return None
    hour, minute = (int(p) for p in until_hhmm.split(":"))
    candidate = datetime.now().replace(hour=hour, minute=minute, second=0, microsecond=0)
    if candidate <= datetime.now():
        candidate += timedelta(days=1)
    return candidate


def run(until_hhmm: str | None = None, max_items: int | None = None,
        dry_run: bool = False, item_id: str | None = None) -> int:
    cfg = config.load()
    config.ensure_init()
    until = resolve_until(until_hhmm)
    registry = sessions.load()
    env, removed = claude_runner.stripped_env(dict(os.environ))
    if removed:
        print(f"csm: stripped billing-capable env vars from claude subprocess: {', '.join(removed)}")

    items = queuefile.load_items()
    if item_id:
        item, err = queuefile.find_item(items, item_id)
        if err:
            print(f"csm: {err}")
            return 1
        if item["status"] != queuefile.PENDING:
            print(f"csm: [{item['id']}] is {item['status']}, not pending "
                  f"(use `csm requeue {item['id']}` to run it again)")
            return 1
        todo = [item]
    else:
        todo = queuefile.pending_items(items)
    if not todo:
        print("csm: queue is empty - nothing to run")
        return 0

    if not dry_run:
        report.append_run_header("manual" if until is None else f"scheduled until {until:%H:%M}")

    ran = 0
    for item in todo:
        if until and datetime.now() >= until:
            print(f"csm: reached --until {until:%H:%M}, stopping")
            break
        if max_items is not None and ran >= max_items:
            print(f"csm: reached --max-items {max_items}, stopping")
            break

        spend = ledger.weekly_spend()
        if spend >= cfg["weekly_token_budget"]:
            msg = (f"weekly budget reached ({spend:,} / {cfg['weekly_token_budget']:,} "
                   "weighted tokens in the last 7 days)")
            print(f"csm: {msg}, stopping")
            if not dry_run:
                report.append_note(msg)
            break

        resume_id = None
        if not item.get("force_new_session"):
            resume_id = sessions.resumable_session(registry, cfg, item["project"])

        session = f"resume {resume_id}" if resume_id else "new session (context.md pickup)"
        model, effort, run_mode = claude_runner.item_settings(cfg, item)
        print(f"csm: [{item['id']}] {item['project']} | {session} | "
              f"model={model} effort={effort or 'cli-default'} mode={run_mode}")
        if dry_run:
            ran += 1
            continue

        result = claude_runner.run_item(cfg, item, resume_id, env)
        ran += 1

        if result.auth_error:
            print(f"csm: {result.error} - stopping the whole run (no quota burned on a broken login)")
            report.append_note(f"run aborted: {result.error}")
            return 2

        if result.rate_limited:
            print("csm: usage limit reached")
            report.append_note("usage limit reached"
                               + (f", resets ~{result.reset_at:%H:%M}" if result.reset_at else ""))
            if result.reset_at and until and result.reset_at < until:
                wait = (result.reset_at - datetime.now()).total_seconds() + 60
                print(f"csm: sleeping {wait / 60:.0f} min until reset (~{result.reset_at:%H:%M})")
                time.sleep(max(wait, 60))
                continue  # item stays pending, retry after reset
            print("csm: no reset time inside the run window - stopping")
            return 3

        _record_outcome(cfg, items, item, result, registry)

    print(f"csm: done ({ran} item(s) processed)")
    return 0


def _record_outcome(cfg, items, item, result, registry) -> None:
    if result.session_id:
        sessions.record(registry, item["project"], result.session_id, result.context_tokens)

    if result.usage:
        ledger.append(item["id"], item["project"],
                      item.get("model") or cfg["default_model"], result.usage)
    weighted = ledger.weighted(result.usage) if result.usage else 0
    spend = ledger.weekly_spend()

    status = queuefile.DONE if result.ok else queuefile.NEEDS_ATTENTION
    queuefile.finish_item(items, item, status, session_id=result.session_id,
                          summary=result.summary, error=result.error,
                          tokens=result.usage or None)
    report.append_item(item, status, result.session_id, result.summary,
                       result.error, weighted, spend, cfg["weekly_token_budget"])

    tag = "done" if result.ok else "NEEDS ATTENTION"
    detail = result.summary or result.error or ""
    print(f"csm: [{item['id']}] {tag} - {detail}")

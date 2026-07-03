# Claude Session Manager (csm) — Implementation Plan

A CLI tool that spends leftover **Claude Pro subscription** quota by running queued
prompts through **Claude Code headless mode** (`claude -p`), during configured quiet
hours (automatically, via Windows Task Scheduler) or on demand (manually).

## Decisions (from design interview)

| Area | Decision |
|---|---|
| Executor | Claude Code headless (`claude -p` / `claude -r <session-id> -p`) with **subscription auth only** — never the pay-per-token API |
| Billing safety | Strip `ANTHROPIC_API_KEY`, `ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, `ANTHROPIC_PROFILE` (and Bedrock/Vertex flags) from the subprocess environment so the CLI can only use the Pro OAuth login. Zero money spent. |
| Weekly cap guard | The tool keeps its own token ledger and stops when its rolling 7-day spend exceeds a configured budget (`weekly_token_budget`) — leftover 5h quota is not free, it draws down the weekly cap too |
| Quota signal | Run-until-limited: attempt the next item; when the CLI returns a usage-limit error, parse the reset time and park until then (or exit). No unofficial endpoints. |
| Window overlap | Accepted: night windows may overlap morning work hours (no protection, per user choice) |
| Queue | Central `queue.json` in `~/.csm/`, managed via `csm add` / `csm status`. Each item: prompt, project dir, optional model override, priority |
| Permissions | Allowlist: file edits, tests, git add/commit/branch — **push, reset, clean, rm blocked** via `--disallowedTools`. Work lands on `csm/<slug>` branches for morning review |
| Failure policy | Park-and-move-on: failures / clarifying questions / mid-task limit cuts mark the item `needs_attention` (session id saved for `claude -r`), no auto-retries |
| Reporting | Markdown run report appended per item: status, project, session id, summary, token spend |
| Runtime | Plain CLI + Windows Task Scheduler (`csm schedule` registers a nightly wake-to-run task). No daemon. |
| Model | `sonnet` default (Pro has no Opus in Claude Code), per-item `--model` override |
| Stack | Python, stdlib only (argparse, json, subprocess). No dependencies. |

## Token optimization & context rotation

1. **Sonnet by default** — stretches each 5-hour window across more queue items.
2. **Context rotation at 40%**: after every run, the tool records the session's
   approximate context size (`input + cache_read + cache_creation + output` tokens
   from `--output-format json`). When a project's session exceeds
   `context_rotate_pct` (default 40%) of `context_window_tokens` (default 200k),
   the next task for that project **starts a fresh session** instead of resuming.
3. **`context.md` handoff protocol**: every prompt is wrapped with rules telling
   Claude to (a) read `./context.md` first if it exists, (b) update it before
   finishing (state, decisions, remaining work, active branch, ≤150 lines).
   A fresh session therefore picks up exactly where the rotated one left off.
4. **Lean prompt wrapper** — the protocol block is short and constant.

## Architecture

```
csm/                      # stdlib-only Python package
  cli.py                  # argparse entry point, subcommands
  config.py               # ~/.csm/config.json load/save + defaults
  queuefile.py            # queue.json CRUD (items, statuses, ordering)
  sessions.py             # sessions.json: project dir -> {session_id, context_tokens}
  ledger.py               # ledger.json: per-run token records, rolling 7-day spend
  claude_runner.py        # builds & runs the claude subprocess, env stripping,
                          # JSON parsing, rate-limit / auth-error detection
  runner.py               # orchestration loop (budget guard, rotation, parking)
  report.py               # appends ~/.csm/report.md
  schedule.py             # registers/removes the Task Scheduler job (WakeToRun)
```

State lives in `~/.csm/` (config.json, queue.json, sessions.json, ledger.json,
report.md) — not in this repo.

## Commands

| Command | Purpose |
|---|---|
| `csm init` | Create `~/.csm/` with default config |
| `csm add "prompt" --project DIR [--model M] [--priority N] [--new-session]` | Queue a task |
| `csm status` | Queue overview + weekly spend + session registry |
| `csm run [--until HH:MM] [--max-items N] [--dry-run]` | Burn quota now (the manual daytime command); `--until` bounds nightly runs |
| `csm report [--tail N]` | Show the run report |
| `csm schedule [--start HH:MM] [--until HH:MM]` / `--remove` | Register nightly Task Scheduler job with wake-to-run |
| `csm config` | Print config path + contents |

## Runner loop

```
while now < until and pending items exist:
    stop if rolling 7-day ledger spend >= weekly_token_budget
    item = highest priority pending item
    session = sessions[item.project]
    resume = session.id if session.context_tokens < 40% of window else None
    run claude -p (env-stripped, allowlisted tools, timeout)
    if rate-limited: park; sleep until reset if it fits before --until, else stop
    if auth error: stop the whole run (don't burn items)
    if error/question: item -> needs_attention (session id saved)
    else: item -> done (summary parsed from trailing "SUMMARY:" line)
    update session registry (new session_id, context token estimate)
    append ledger record + report entry
```

## Milestones

1. Plan + git init (this file) — commit
2. Package skeleton: config, queue, sessions, ledger — commit
3. Claude runner + orchestration loop + report — commit
4. Scheduler integration + README — commit
5. Smoke test (`csm init`, `csm add`, `csm run --dry-run`) — fix + commit

## Known open items (v1 accepts these)

- Rate-limit message parsing is the one unofficial-ish surface; regexes are
  defensive and the fallback is "stop the run" (safe).
- Branch creation is instructed via prompt, not verified by the tool.
- Weekly budget default (1,000,000 weighted tokens) is a conservative guess for
  Pro — tune it in config after observing a week in `csm status`.

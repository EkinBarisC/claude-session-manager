# csm — Claude Session Manager

Spend leftover **Claude Pro subscription** quota automatically. You queue up
tasks; `csm` runs them through **Claude Code headless mode** during your quiet
hours (via Windows Task Scheduler) or whenever you say so — until the usage
limit hits or its weekly budget is spent.

**No money is ever spent.** csm only uses your existing Pro subscription login.
It strips every API-key / billing environment variable (`ANTHROPIC_API_KEY`,
`ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, Bedrock/Vertex flags, …) from the
`claude` subprocess, so it physically cannot fall back to pay-per-token API
billing. If the CLI isn't logged in, the run aborts instead of spending.

## Requirements

- Windows, Python 3.10+
- [Claude Code](https://claude.com/claude-code) installed and **logged in with
  your Pro account** (run `claude` once, use `/login`, pick the subscription —
  not an API key / Console account)

## Install

```powershell
cd claude-session-manager
pip install -e .
csm init
```

(Or skip the install and use `py -m csm ...` from this directory.)

## Usage

```powershell
# Queue tasks (each targets a project directory)
csm add "Add input validation to the signup form, with tests" --project C:\code\myapp
csm add "Review src/auth for security issues, write findings to REVIEW.md" --project C:\code\myapp --priority 5

# See the queue, weekly spend, and session states
csm status

# Burn leftover quota right now (your manual daytime command)
csm run

# Run only until 08:00 (what the nightly job does)
csm run --until 08:00

# Preview without invoking claude
csm run --dry-run

# Register the nightly job (daily at quiet_hours_start, wakes the PC,
# runs until quiet_hours_end). Needs an elevated/admin PowerShell.
csm schedule
csm schedule --remove

# Read the results over coffee
csm report

# Put a failed item back in the queue
csm requeue <item-id>
```

### First-time validation

Queue one tiny task against a scratch project and run it manually:

```powershell
csm add "Say hello and stop." --project C:\code\scratch
csm run --max-items 1
csm report
```

## How it works

1. **Queue** — `~/.csm/queue.json`. Items run highest-priority first. Each item
   runs `claude -p` inside its project directory.
2. **Sessions & context rotation** — csm remembers the last session per project
   and resumes it (`claude -r`) while its estimated context stays under **40%**
   of the context window. Past that, the next task starts a **fresh session**.
   Continuity survives rotation because every task follows the protocol:
   *read `./context.md` first if it exists; update it before finishing*
   (state, decisions, remaining work, branch — max 150 lines).
3. **Safety** — tasks work on `csm/<slug>` git branches and may edit files, run
   tests, and commit. `git push`, `reset`, `rebase`, `clean`, `rm` are blocked
   via `--disallowedTools`. You review branches in the morning.
4. **Limits** — when Claude Code reports the usage limit, csm parses the reset
   time and sleeps until then (if it fits inside `--until`), else stops. Items
   are never lost — unfinished ones stay pending.
5. **Weekly budget** — every run's token usage lands in `~/.csm/ledger.json`.
   When the rolling 7-day weighted spend passes `weekly_token_budget`, csm
   stops — leftover 5-hour quota still draws down your weekly cap, and the bot
   must not eat capacity you need later in the week.
6. **Failures** — a failed / timed-out / question-asking task is marked
   `needs_attention` with its session id saved; the runner moves on. Pick it up
   with `claude -r <session-id>` or `csm requeue <id>`. No auto-retries.
7. **Report** — `~/.csm/report.md` gets one block per item: status, project,
   session id, one-line summary, and token spend vs budget.

## Configuration — `~/.csm/config.json`

| Key | Default | Meaning |
|---|---|---|
| `default_model` | `sonnet` | Model for items without `--model`. Pro has no Opus in Claude Code. |
| `weekly_token_budget` | `1000000` | Rolling 7-day cap on weighted tokens (input + cache_creation + output + 0.1×cache_read). Tune after watching a week in `csm status`. |
| `context_window_tokens` | `200000` | Model context window size. |
| `context_rotate_pct` | `40` | Rotate to a fresh session past this % of the window. |
| `item_timeout_minutes` | `30` | Hard per-item timeout. |
| `quiet_hours_start` / `quiet_hours_end` | `00:30` / `07:30` | Defaults for `csm schedule`. |
| `allowed_tools` / `disallowed_tools` | see file | Tool allowlist passed to every run. |
| `claude_binary` | `claude` | CLI to invoke. |

## Notes & caveats

- **The PC must be awake.** `csm schedule` sets *wake-to-run*, but check
  Windows power settings allow wake timers (Power Options → Sleep →
  Allow wake timers).
- **5-hour windows start on first message.** A night run opens windows as it
  goes; a window opened at 5 AM lasts until 10 AM and shares quota with your
  morning work. (Accepted trade-off — csm does not protect your mornings.)
- **`total_cost_usd` in logs is informational.** Claude Code prints an
  estimated cost even on subscription auth; nothing is billed.
- **Rate-limit parsing** is the one unofficial surface. If Claude Code changes
  its limit message, csm fails safe: the run stops and items stay pending.
- State lives in `~/.csm/` (override with the `CSM_HOME` env var).

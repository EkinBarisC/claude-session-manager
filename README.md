# csm — Claude Session Manager

Spend leftover **Claude Pro subscription** quota automatically. You queue up
tasks; `csm` runs them through **Claude Code headless mode** during your quiet
hours (Task Scheduler on Windows, cron on macOS/Linux) or whenever you say so —
until the usage limit hits or its weekly budget is spent. A single static Go
binary with both a CLI and an interactive TUI.

**No money is ever spent.** csm only uses your existing Pro subscription login.
It strips every API-key / billing environment variable (`ANTHROPIC_API_KEY`,
`ANTHROPIC_AUTH_TOKEN`, `ANTHROPIC_BASE_URL`, Bedrock/Vertex flags, …) from the
`claude` subprocess, so it physically cannot fall back to pay-per-token API
billing. If the CLI isn't logged in, the run aborts instead of spending.

## Requirements

- Windows, macOS, or Linux
- [Claude Code](https://claude.com/claude-code) installed and **logged in with
  your Pro account** (run `claude` once, use `/login`, pick the subscription —
  not an API key / Console account)

## Install

Grab a binary from the
[releases page](https://github.com/EkinBarisC/claude-session-manager/releases)
and put it on your PATH, or build from source with Go 1.26+:

```sh
go install github.com/EkinBarisC/claude-session-manager/cmd/csm@latest
```

Then:

```sh
csm init
csm doctor               # verifies claude login, config, and the nightly job
```

## TUI

Run `csm` with no arguments (or `csm tui`) for the interactive UI:

- **Queue tab** — browse items; `n` queue a new task, `enter` full detail,
  `R` run the selected item (live spinner, result lands in the table),
  `r` requeue, `d` delete (with confirm), `u` refresh.
- **Report tab** — scroll the run report.
- **Config tab** — view the effective config.
- Status bar shows rolling weekly spend against the budget, color-coded.

## CLI usage

Every command supports `-h`/`--help`. Item ids can be abbreviated to any
unique prefix (`csm show ff3`).

```sh
# Queue tasks. --project/-C defaults to the current directory.
csm add "Add input validation to the signup form, with tests"
csm add "Review src/auth, write findings to REVIEW.md" -C ../myapp --priority 5

# Per-item model, effort, and run mode (fall back to config defaults)
csm add "Plan the v2 schema migration" --mode plan --effort high
csm add "Bulk-rename fixtures" -m haiku --effort low

# Inspect and manage the queue
csm status               # overview: queue, weekly spend, sessions
csm list                 # pending + failed items (ls works too; --all for everything)
csm show <id>            # one item in full
csm edit <id> --priority 9 --effort max
csm rm <id> [<id>...]    # delete items
csm clear                # drop done items (--all wipes the queue)
csm requeue <id>         # put failed items back (--all for every failure)

# Run
csm run                  # burn leftover quota right now
csm run --until 08:00    # what the nightly job does
csm run --id <id>        # run one specific item
csm run --dry-run        # preview without invoking claude

# Settings from the CLI
csm config               # show effective config
csm config set default_effort low
csm config set weekly_token_budget 2000000
csm config unset default_effort
csm config edit          # open config.json in your editor

# Register the nightly job (daily at quiet_hours_start, runs until
# quiet_hours_end). Windows: Task Scheduler with wake-to-run, needs an
# elevated/admin PowerShell. macOS/Linux: a crontab entry.
csm schedule
csm schedule --remove

# Read the results over coffee
csm report
```

### Run modes

| Mode | Meaning |
|---|---|
| `plan` | Read-only planning (`--permission-mode plan`). Good for design/review tasks. |
| `safe` | Default. Edits, tests, and branch-local git via the config allowlist; push and destructive commands blocked. |
| `full` | `--dangerously-skip-permissions`. Only for sandboxed/throwaway directories. |

Effort (`low`–`max`) maps to `claude --effort`; lower effort stretches your
quota across more items. Both have config-wide defaults (`default_run_mode`,
`default_effort`) and per-item overrides on `csm add`/`csm edit`.

### First-time validation

Queue one tiny task against a scratch project and run it manually:

```sh
csm add "Say hello and stop." --project ../scratch
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

Edit via `csm config set/unset/edit`, or by hand (`csm config path`).

| Key | Default | Meaning |
|---|---|---|
| `default_model` | `sonnet` | Model for items without `--model`. Pro has no Opus in Claude Code. |
| `default_effort` | `medium` | `claude --effort` for items without `--effort` (`low`\|`medium`\|`high`\|`xhigh`\|`max`, or `""` for the CLI default). |
| `default_run_mode` | `safe` | Run mode for items without `--mode` (`plan`\|`safe`\|`full`). |
| `weekly_token_budget` | `1000000` | Rolling 7-day cap on weighted tokens (input + cache_creation + output + 0.1×cache_read). Tune after watching a week in `csm status`. |
| `context_window_tokens` | `200000` | Model context window size. |
| `context_rotate_pct` | `40` | Rotate to a fresh session past this % of the window. |
| `item_timeout_minutes` | `30` | Hard per-item timeout. |
| `quiet_hours_start` / `quiet_hours_end` | `00:30` / `07:30` | Defaults for `csm schedule`. |
| `allowed_tools` / `disallowed_tools` | see file | Tool allowlist passed to every run. |
| `claude_binary` | `claude` | CLI to invoke. |

## Notes & caveats

- **The machine must be awake.** On Windows, `csm schedule` sets *wake-to-run* —
  check power settings allow wake timers (Power Options → Sleep → Allow wake
  timers). cron does **not** wake a sleeping machine: on macOS set a wake alarm
  (`sudo pmset repeat wakeorpoweron ...`) or keep the lid open; on Linux use
  `rtcwake` or disable suspend for the night.
- **5-hour windows start on first message.** A night run opens windows as it
  goes; a window opened at 5 AM lasts until 10 AM and shares quota with your
  morning work. (Accepted trade-off — csm does not protect your mornings.)
- **`total_cost_usd` in logs is informational.** Claude Code prints an
  estimated cost even on subscription auth; nothing is billed.
- **Rate-limit parsing** is the one unofficial surface. If Claude Code changes
  its limit message, csm fails safe: the run stops and items stay pending.
- State lives in `~/.csm/` (override with the `CSM_HOME` env var). The state
  files are compatible with the earlier Python version of csm — an existing
  queue, ledger, and session registry carry over as-is.

## Contributing

Issues and PRs welcome. To develop locally (Go 1.26+):

```sh
git clone https://github.com/EkinBarisC/claude-session-manager
cd claude-session-manager
go test ./...
go build -o csm ./cmd/csm
CSM_HOME=/tmp/csm-dev ./csm status    # keep your real queue out of the way
```

CI runs vet, tests, and a headless smoke test on Windows, Ubuntu, and macOS
(no Claude login needed) — see
[.github/workflows/ci.yml](.github/workflows/ci.yml). Releases are cut by
pushing a `v*` tag, which cross-compiles binaries for all six OS/arch pairs
and publishes a GitHub release.

## License

[MIT](LICENSE)

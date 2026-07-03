"""Runs one queue item through `claude -p` and interprets the outcome.

Billing safety: the subprocess environment is stripped of every variable
that could route the Claude Code CLI to pay-per-token API billing or a
third-party provider. With those gone the CLI can only use the stored
subscription (Pro) OAuth login. If it isn't logged in, the run fails with
an auth error instead of spending money.
"""

import json
import re
import shutil
import subprocess
from dataclasses import dataclass, field
from datetime import datetime, timedelta

# Any of these could redirect the CLI to usage-based billing.
STRIP_ENV = (
    "ANTHROPIC_API_KEY",
    "ANTHROPIC_AUTH_TOKEN",
    "ANTHROPIC_BASE_URL",
    "ANTHROPIC_PROFILE",
    "ANTHROPIC_MODEL",
    "CLAUDE_CODE_USE_BEDROCK",
    "CLAUDE_CODE_USE_VERTEX",
    "AWS_BEARER_TOKEN_BEDROCK",
)

# Appended to every queued prompt. Keeps sessions cheap to rotate: state
# always lives in ./context.md, so a fresh session can pick up mid-stream.
PROTOCOL = """
---
Automated run rules (csm):
1. If ./context.md exists in the project root, read it first for prior state.
2. Do all work on a git branch named csm/<short-task-slug> (create it if
   needed). Commit your changes. NEVER push, force-reset, or delete branches.
3. Before finishing, create or update ./context.md (max 150 lines): current
   state, key decisions, remaining work, and the active branch name.
4. End your reply with exactly one line: SUMMARY: <one sentence result>.
"""

RATE_LIMIT_RE = re.compile(
    r"usage limit|rate limit|limit reached|limit will reset|out of extra usage",
    re.IGNORECASE,
)
AUTH_ERROR_RE = re.compile(
    r"/login|not logged in|invalid api key|api key not found|oauth token|"
    r"authentication_error|please log in",
    re.IGNORECASE,
)
# Claude Code prints e.g. "Claude AI usage limit reached|1712345678"
EPOCH_RE = re.compile(r"\|(\d{10})\b")
RESET_AT_RE = re.compile(r"reset(?:s)?\s+(?:at\s+)?(\d{1,2})(?::(\d{2}))?\s*(am|pm)?", re.IGNORECASE)


@dataclass
class RunResult:
    ok: bool = False
    rate_limited: bool = False
    auth_error: bool = False
    timed_out: bool = False
    reset_at: datetime | None = None
    session_id: str | None = None
    usage: dict = field(default_factory=dict)
    result_text: str = ""
    summary: str | None = None
    error: str | None = None

    @property
    def context_tokens(self) -> int:
        u = self.usage
        return (
            u.get("input_tokens", 0)
            + u.get("cache_creation_input_tokens", 0)
            + u.get("cache_read_input_tokens", 0)
            + u.get("output_tokens", 0)
        )


def stripped_env(base: dict) -> tuple[dict, list]:
    env = dict(base)
    removed = [k for k in STRIP_ENV if env.pop(k, None) is not None]
    return env, removed


def build_command(cfg: dict, prompt: str, model: str, resume_id: str | None) -> list:
    binary = shutil.which(cfg["claude_binary"])
    if not binary:
        raise SystemExit(
            f"csm: '{cfg['claude_binary']}' not found on PATH. Install Claude Code "
            "and log in with your Pro account first."
        )
    cmd = [binary]
    if resume_id:
        cmd += ["--resume", resume_id]
    cmd += [
        "-p", prompt + PROTOCOL,
        "--output-format", "json",
        "--model", model,
    ]
    if cfg["allowed_tools"]:
        cmd += ["--allowedTools", ",".join(cfg["allowed_tools"])]
    if cfg["disallowed_tools"]:
        cmd += ["--disallowedTools", ",".join(cfg["disallowed_tools"])]
    return cmd


def parse_reset_time(text: str) -> datetime | None:
    m = EPOCH_RE.search(text)
    if m:
        try:
            return datetime.fromtimestamp(int(m.group(1)))
        except (ValueError, OSError):
            pass
    m = RESET_AT_RE.search(text)
    if m:
        hour = int(m.group(1))
        minute = int(m.group(2) or 0)
        meridiem = (m.group(3) or "").lower()
        if meridiem == "pm" and hour < 12:
            hour += 12
        if meridiem == "am" and hour == 12:
            hour = 0
        if hour > 23 or minute > 59:
            return None
        candidate = datetime.now().replace(hour=hour, minute=minute, second=0, microsecond=0)
        if candidate <= datetime.now():
            candidate += timedelta(days=1)
        return candidate
    return None


def _extract_summary(text: str) -> str | None:
    for line in reversed(text.strip().splitlines()):
        if line.strip().upper().startswith("SUMMARY:"):
            return line.strip()[len("SUMMARY:"):].strip()
    return None


def run_item(cfg: dict, item: dict, resume_id: str | None, env: dict) -> RunResult:
    model = item.get("model") or cfg["default_model"]
    cmd = build_command(cfg, item["prompt"], model, resume_id)
    res = RunResult()
    try:
        proc = subprocess.run(
            cmd,
            cwd=item["project"],
            env=env,
            capture_output=True,
            text=True,
            encoding="utf-8",
            errors="replace",
            timeout=cfg["item_timeout_minutes"] * 60,
        )
    except subprocess.TimeoutExpired:
        res.timed_out = True
        res.error = f"timed out after {cfg['item_timeout_minutes']} minutes"
        return res
    except OSError as exc:
        res.error = f"failed to start claude: {exc}"
        return res

    combined = (proc.stdout or "") + "\n" + (proc.stderr or "")

    payload = _parse_json_payload(proc.stdout or "")
    if payload:
        res.session_id = payload.get("session_id")
        res.usage = payload.get("usage") or {}
        res.result_text = payload.get("result") or ""

    if AUTH_ERROR_RE.search(combined):
        res.auth_error = True
        res.error = "auth error - claude is not logged in with the subscription account"
        return res
    if RATE_LIMIT_RE.search(combined):
        res.rate_limited = True
        res.reset_at = parse_reset_time(combined)
        res.error = "usage limit reached"
        return res

    if proc.returncode != 0 or (payload or {}).get("is_error"):
        res.error = _first_line(res.result_text or proc.stderr or proc.stdout or "unknown error")
        return res

    res.ok = True
    res.summary = _extract_summary(res.result_text)
    # A run that ends by asking us something needs a human, not the queue.
    last_line = res.result_text.strip().splitlines()[-1] if res.result_text.strip() else ""
    if res.summary is None and last_line.endswith("?"):
        res.ok = False
        res.error = "ended with a question: " + _first_line(last_line)
    return res


def _parse_json_payload(stdout: str) -> dict | None:
    text = stdout.strip()
    if not text:
        return None
    start = text.find("{")
    if start == -1:
        return None
    try:
        return json.loads(text[start:])
    except json.JSONDecodeError:
        return None


def _first_line(text: str, limit: int = 200) -> str:
    line = text.strip().splitlines()[0] if text.strip() else ""
    return line[:limit]

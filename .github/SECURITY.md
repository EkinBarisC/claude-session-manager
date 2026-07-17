# Security Policy

Thanks for helping keep **csm (Claude Session Manager)** and its users safe.

## Scope

csm is a small tool that runs queued tasks through Claude Code headless mode
against your existing Claude Pro subscription. Please keep in mind:

- Running csm against **untrusted repositories** or with **untrusted prompts**
  is inherently risky — the tasks you queue are executed by Claude Code with
  whatever permissions you grant it. Treat inputs you don't control as you would
  any code you're about to run locally.
- Reports that require an attacker to supply crafted prompts or malicious repo
  contents are still in scope and welcome — we'd rather patch them.

## Reporting a Vulnerability

**Please do not open a public GitHub issue for security vulnerabilities.**

Instead, report privately through **GitHub private vulnerability reporting**:
go to the [Security tab](https://github.com/EkinBarisC/claude-session-manager/security)
and click **"Report a vulnerability"**.

Please include, as much as you can:

- A description of the vulnerability and its impact.
- Steps to reproduce (proof-of-concept, affected commit/version, OS).
- Any suggested fix or mitigation.

## What to Expect

This is a hobby / side project maintained in spare time, so please allow for
best-effort response times:

- **Acknowledgement:** within about 7 days.
- **Assessment & fix:** timeline communicated after triage, depending on
  severity and complexity.
- **Disclosure:** please give me a reasonable chance to release a fix before
  any public disclosure. Credit will gladly be given to reporters who want it.

## Supported Versions

csm is developed on the `main` branch. Security fixes are applied to `main`;
there are currently no separately maintained release branches.

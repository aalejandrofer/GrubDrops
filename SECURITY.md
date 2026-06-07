# Security Policy

GrubDrops is self-hosted and single-tenant: it holds your Twitch/Kick
session material (age-encrypted on disk) and talks to those platforms as you.
Run it on infrastructure you control.

## Reporting a vulnerability

Please **do not** open a public issue for security problems. Instead, report
privately via GitHub's **Security → Report a vulnerability** (private advisory)
on this repository. Include repro steps and impact. I'll acknowledge and work
on a fix as time permits — this is a hobby project, not a funded one.

## Handling secrets

- Never paste real cookies, tokens, `MINER_MASTER_KEY` values, or Discord
  webhook URLs into issues, PRs, or logs.
- `.env` is gitignored; only `.env.example` (placeholders) is tracked.

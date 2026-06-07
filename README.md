<p align="center">
  <img src="internal/web/static/img/logo.svg" width="88" alt="GrubDrops logo">
</p>

<h1 align="center">GrubDrops</h1>

<p align="center"><b>Self-hosted Twitch &amp; Kick drops miner.</b><br>
Pick your games, it watches the right streams, mines the drops, and claims them — hands-free.</p>

<p align="center">
  <img alt="Go" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white">
  <img alt="Twitch" src="https://img.shields.io/badge/Twitch-drops-9146FF?logo=twitch&logoColor=white">
  <img alt="Kick" src="https://img.shields.io/badge/Kick-drops-53FC18?logo=kick&logoColor=black">
  <img alt="UI" src="https://img.shields.io/badge/UI-HTMX%20%2B%20Go%20templates-2c2c2c">
  <img alt="Storage" src="https://img.shields.io/badge/DB-SQLite-003B57?logo=sqlite&logoColor=white">
  <img alt="Self-hosted" src="https://img.shields.io/badge/self--hosted-Docker-2496ED?logo=docker&logoColor=white">
</p>

---

**Keywords:** twitch drops miner · kick drops miner · self-hosted · multi-account · headless · auto-claim

## Why

A whitelist-driven drops miner for **both Twitch and Kick**, multiple accounts each, from one dashboard. You choose the games; it never touches anything else.

## Features

- 🎯 **Whitelist-driven** — global priority list or per-account; only your games are mined.
- 🟣🟢 **Twitch + Kick**, multi-account — watch → mine → claim, automatically.
- 🔗 **Connection-aware** — campaigns needing an external link (Krafton, Embark…) are flagged per-account and skipped until linked.
- 🖥️ **Live console** — real-time event feed, currently-mining panel, `/drops` catalog, auth-health checks.
- 🔔 **Discord** notifications with rich embeds (per-kind toggles).

## How

- **Twitch** — official **device-code** OAuth (no password/cookies); GraphQL + PubSub for real-time progress/claims.
- **Kick** — pure-HTTP client with a real **Chrome TLS fingerprint** (`utls`) that walks past Cloudflare — no browser, no `cf_clearance`.

## Stack

Go · `html/template` + HTMX (no JS build) · SQLite (sqlc + goose) · age-encrypted sessions · Docker.

## Quick start

```bash
go build ./cmd/miner
MINER_MASTER_KEY=$(head -c32 /dev/urandom | base64) ./miner   # → http://localhost:8080
```

First run creates the admin login. See [`CHANGELOG.md`](CHANGELOG.md) for what's new.

---

<sub>Built by [@aalejandrofer](https://github.com/aalejandrofer) with [Claude Code](https://claude.com/claude-code) (Opus 4.8).</sub>


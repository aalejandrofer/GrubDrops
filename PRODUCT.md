# Product

<!-- impeccable:product-schema 1 -->

## Platform

web

## Stack

v2: SvelteKit static SPA (TypeScript) embedded in the Go binary; Go serves a JSON API and Server-Sent Events. See `docs/superpowers/specs/2026-10-25-grubdrops-v2-rebuild-design.md`.

## Users

Public self-hosters who find GrubDrops on GitHub or Discord and run it on a NAS or VPS. They farm Twitch and Kick drops for games they choose. Usage is set and forget: they set it up once, then open it only when a notification says something is wrong or a drop was claimed.

## Product Purpose

GrubDrops watches Twitch and Kick streams on the user's accounts so watch-time drops accrue and get claimed without the user watching. Success is drops landing in the user's inventory with no attention needed. When they do open the app, it answers "is it working?" within seconds.

## Positioning

- Twitch and Kick in one daemon. Kick watch-time comes from real Chrome playback in on-demand sidecars.
- Many accounts mined in parallel from one install.
- Self-hosted: Docker plus a browser UI on the user's own server, not a desktop app.
- Choose-your-games: the user picks games, GrubDrops mines only those and auto-claims.

## Operating Context

- Runs as a Docker container (image on ghcr.io); Kick sidecars start on demand over the Docker socket.
- Notifications currently go to Discord (`GRUB_DISCORD_WEBHOOK`); Telegram is planned.
- Twitch login is the TV-token flow; the Android device-code flow is blocked by Twitch.
- Twitch campaign listing is limited, so discovery runs only for games the user chose.

## Capabilities and Constraints

- Pages for the rebuild: Home, Games, Rewards, Settings. Accounts and the live event log (Health) live inside Settings.
- Games page priority is the permanent mining order. Home may allow a temporary reorder of what mines next.
- More chosen games means a slower scan; that is the user's trade-off and the UI shows it.
- The mining core (Twitch 60s beacon, Kick WS + IVS playback, browser sidecars, auth, encrypted sessions) is kept as-is. Discovery, collection (claim, progress, inventory) and the UI are rebuilt.
- All user-facing strings go through `internal/i18n`; every locale keeps an equal key set.

## Brand Commitments

- Name: GrubDrops.
- Logo: the grub mascot, `assets/img/logo.png`. Keep it.
- Purple is the brand accent and stays.

## Evidence on Hand

- Logo and icons in `assets/img/`.
- Rebuild mockups in `sandbox/home-rebuild/` (round 1: 10 Home styles; round 2: 08, 09, 03 in full plus combined Homes).
- No user testimonials, usage numbers or press exist; do not invent them.

## Product Principles

1. Answer "is it working?" first. Everything else is one click away.
2. Fewer buttons, more direct. Show only what needs the user.
3. Problems come with one clear fix, not a log to read.
4. The user's game choice drives everything the app does.

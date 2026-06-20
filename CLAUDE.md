# GrubDrops project rules

## Releasing

A commit is **not** a release. Day-to-day work lands on `master` freely.

1. **Log every change to `docs/CHANGELOG.md`** under `## [Unreleased]`
   (Added / Changed / Fixed / Removed) as you commit — every bug fix, addition,
   removal, and change, no matter how small. This is the complete running record.
2. **Verify before cutting a version.** Live drops are rarely available to test
   against, so the gate is tiered:
   - **Default gate (no live drop available):** a green build + passing unit tests
     + a green Kick WS transport canary (Heartbeat Health Checker). This is enough
     to tag changes that do **not** touch accrual/claim logic (UI, settings,
     watcher lifecycle, discovery, plumbing). Cover the fix with a unit test —
     that's the durable proof, since most of the time a live drop can't be.
   - **Accrual/claim changes:** when a change touches how watch-time accrues or how
     a drop is claimed (Twitch beacon, Kick WS/IVS watch path, claim flow), confirm
     against a **live drop** before tagging (Twitch and/or Kick, whichever the
     change touches). If no live drop is available, hold the tag in `[Unreleased]`
     until one is — do not tag accrual changes on unit tests alone.
3. Once verified: move `[Unreleased]` to the new version in `docs/CHANGELOG.md`, push
   the `v*` tag (this triggers the ghcr image build), and write the patch notes
   in the GitHub **Releases** tab — **cherry-pick** the user-facing highlights
   from `[Unreleased]`. The release notes are a curated subset, not the whole
   changelog.

A broken miner must never ship under a version tag.

### Release notes format

Never a paragraph wall. Lead with a one-line **highlight**, then group entries
under these emoji section headers, in this order (omit empty sections):

- **🌱 Added** — new features
- **⚙️ Changed** — behaviour / UX changes
- **🐛 Fixed** — bug fixes
- **🔥 Removed** — removed features, flags, or paths

Bold the subject of each bullet; write what changed + why it matters in plain
user-facing language, not commit-speak. The notes are a curated subset of
`[Unreleased]`, not the raw changelog.

Release **title** = bare version only (e.g. `v1.2.5`). Release **notes** are
humanized (run the humanizer skill): plain voice, **no em/en dashes**, but keep
the emoji-section + bold-subject format above.

## Architecture constraints

- **The browser sidecar stack is core, not dead code.** Do not delete
  `cmd/browser-sidecar`, `internal/auth/browser`, `internal/dockerctl`,
  `proto/` + buf config, or the Twitch `BrowserBackend`. Kick watch-time (IVS
  playback credit) depends on the on-demand chromedp sidecars. Cleanup passes
  have wrongly flagged these as removable — leave them.
- **Stay Go + html/template + HTMX.** A React/Vite port was tried and scrapped
  (2026-06-06). Do not re-propose a JS port; build features in templates.

## Platform gotchas

- **Twitch = Android device-code OAuth over direct HTTP.** Never send a
  `Client-Integrity` header; adding it was a self-inflicted integrity wall.
  Don't re-implement logged-out catalog scraping either — `/drops/campaigns`
  while logged out is just a login wall.
- **Twitch credit beacon is gated by `HeartbeatInterval`** (1 minute credited
  per beacon), so anything above 60s under-credits. Keep it at 60s. Kick is
  immune.

## sqlc

- **Never put `?` or parentheses in a `queries/*.sql` comment.** It corrupts
  placeholder rewriting for later queries, producing a cryptic SQL syntax error.

## Security scanning

- **CodeQL runs on every push to `master`** (GitHub default setup — there is no
  workflow file). Keep it at zero open alerts.
- **Never concatenate outside-controlled data into a chromedp `Evaluate` script.**
  Game names, drop titles, and channel names can be attacker-influenced; embed
  them base64-encoded (`jsB64JSON`) and decode in-script with `TextDecoder`, so
  no input byte can break out of the eval literal.

## See also

- **`AGENTS.md`** — build/test/run commands, repo layout, and the same
  architecture + platform constraints, for AI coding agents. Keep the shared
  sections (architecture, platform gotchas, sqlc) in sync with this file.

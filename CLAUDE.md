# GrubDrops project rules

## Releasing

A commit is **not** a release. Day-to-day work lands on `master` freely.

1. **Log every change to `CHANGELOG.md`** under `## [Unreleased]`
   (Added / Changed / Fixed / Removed) as you commit — every bug fix, addition,
   removal, and change, no matter how small. This is the complete running record.
2. **Cut a version only after verifying drop-mining works.** A green build and
   passing tests are not enough — confirm the miner actually accrues watch-time
   and claims a drop (Twitch and/or Kick, whichever the change touches).
3. Once verified: move `[Unreleased]` to the new version in `CHANGELOG.md`, push
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

# GrubDrops project rules

## Releasing

A commit is **not** a release. Day-to-day work lands on `master` freely.

1. **Log every change** under the `## [Unreleased]` section of `CHANGELOG.md`
   (Added / Changed / Fixed / Removed) as you commit.
2. **Cut a version only after verifying drop-mining works.** A green build and
   passing tests are not enough — confirm the miner actually accrues watch-time
   and claims a drop (Twitch and/or Kick, whichever the change touches).
3. Once verified: move `[Unreleased]` to the new version in `CHANGELOG.md`, push
   the `v*` tag (this triggers the ghcr image build), and write the patch notes
   in the GitHub **Releases** tab.

A broken miner must never ship under a version tag.

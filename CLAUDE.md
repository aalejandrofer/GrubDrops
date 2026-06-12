# GrubDrops project rules

## Releasing

- **Never push a new version (a `v*` release tag) until drop-mining is verified
  working.** A green build and passing tests are not enough; confirm the miner
  actually accrues watch-time and claims a drop (Twitch and/or Kick, whichever
  the change touches) before tagging. Tags trigger the ghcr image build and mark
  a public release, so a broken miner must never ship under a version.

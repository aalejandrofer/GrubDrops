# On-demand Kick sidecars — stop browser containers when idle

**Date:** 2026-06-12
**Status:** Approved (design), pending implementation plan
**Branch:** builds on `agent-a12f334b3f499cfa9` (per-account sidecar pool, commit `7fe74ac`)

## Problem

Each Kick account watches via its own chromedp sidecar (a real IVS `<video>`
playing Chrome ≈ 0.4–0.6 GiB RAM). The host has no swap. Today both sidecars
run 24/7 even when there is nothing to mine — between Kick drop events all
campaigns are expired, yet two Chromes sit idle holding ~1 GiB.

Detection (campaigns, eligible-channel liveness, progress) is 100% pure-HTTP
over the utls transport and needs no browser. Only `StartWatch`/`Heartbeat`
(the IVS playback) need the sidecar. So the daemon can keep polling with
sidecars stopped and only spin one up when there is actually a live channel to
watch for that account.

## Goal

Per-account: stop an account's sidecar container when that account has nothing
watchable for a grace period; start it on demand when a watchable live channel
appears. Free Chrome RAM during lulls and entirely between events. Never block
or break mining; degrade gracefully if Docker control is unavailable.

## Non-goals

- No change to the accrual mechanism (still browser IVS playback).
- No change to Twitch (utls/device-code, no browser).
- No autoscaling beyond the configured per-account sidecars.
- Not `docker pause` (that freezes the process but keeps RAM resident). We use
  `docker stop` / `docker start` to actually reclaim memory.

## Configuration

Per-account sidecar identity is **auto-derived from the account username** via a
template, so adding an account needs no env edit — only a matching compose
container.

```
GRUB_KICK_SIDECAR_TEMPLATE=grubdrops-browser-{slug}   # default; port appended as :9090
GRUB_KICK_SIDECAR_PORT=9090                            # default
```

- `slug` = the account's `display_name` lowercased, with every char outside
  `[a-z0-9-]` collapsed to `-` and runs trimmed (e.g. `TTik3r`→`ttik3r`,
  `Phluses`→`phluses`). Deterministic.
- The derived string `grubdrops-browser-<slug>` is used for ALL THREE: the gRPC
  dial host (compose DNS), `docker start`, and `docker stop`. The port forms the
  gRPC URL `…-<slug>:9090`.
- **Coupling (must hold):** the compose `container_name` of each sidecar MUST
  equal the derived slug. Drift = daemon dials/controls a nonexistent name.
  Deterministic slugify + operator-controlled compose keeps them in lockstep.
- gRPC dial is lazy (`grpc.NewClient` connects on first RPC), so a stopped
  on-demand container is fine until the first `StartWatch`.
- On-demand stop/start is ENABLED when `GRUB_KICK_BROWSER_WATCH=1` AND the
  docker socket is controllable (see degrade path). When the socket is
  unavailable the daemon still dials the derived URLs but never stops them
  (always-on, today's behavior).
- Fallback (no regression): the existing `GRUB_BROWSER_URLS` / `GRUB_BROWSER_URL`
  remain the login / Twitch / display client and the watch path if no per-account
  template resolves.

## Components

### 1. `dockerctl` (new, `internal/dockerctl`)
Thin wrapper over the Docker Engine SDK (`github.com/docker/docker/client`),
talking to a mounted `/var/run/docker.sock`. Single purpose: control a
container by name.

- `New() (*Client, error)` — `client.NewClientWithOpts(FromEnv, WithAPIVersionNegotiation)`. Returns error if the socket is unreachable.
- `Start(ctx, name) error` — idempotent (`ContainerStart`; no-op if already running).
- `Stop(ctx, name, timeout) error` — idempotent (`ContainerStop`).
- `Running(ctx, name) (bool, error)` — `ContainerInspect`, reads `State.Running`.

Testable against the interface; no daemon logic inside.

### 2. Kick backend sidecar registry (extend `internal/platform/kick`)
Replace the anonymous `watchPool`/`clientByAcc` round-robin with a registry
keyed by accountID, each entry derived from the account username via the
template (slug → `grubdrops-browser-<slug>` + port). The backend is told each
Kick account's `(accountID, username)` at registration (the login handler and
startup account enumeration already have both):

```
type sidecar struct {
    grpcURL       string
    containerName string
    client        *browser.Client
    mu            sync.Mutex
    idleSince     time.Time // zero = not idle
}
```

- `accountID -> *sidecar` map, set at construction.
- `watchClientFor(accountID)` returns the account's pinned client (unchanged
  contract for `StartWatch`); when a per-account sidecar is derived the
  client is still dialed once at startup and reused (gRPC reconnects across
  container restarts on its own).

### 3. On-demand lifecycle (no watcher changes)
Activity is inferred from existing watch traffic — no new watcher hooks:

- Each `sidecar` tracks `lastActive time.Time`, bumped on every `StartWatch`
  AND `Heartbeat` for that account. A watching account heartbeats ~every 60s,
  so `lastActive` stays fresh; an account with nothing watchable idles in
  `pick_campaign` (no StartWatch/Heartbeat) so `lastActive` goes stale.
- `EnsureSidecarUp(accountID)` — called inside `StartWatch` BEFORE the gRPC
  call. If a controllable sidecar is configured: `dockerctl.Start` (idempotent),
  then poll readiness via `client.Heartbeat(ctx, "")` (nil error = server up;
  the sidecar returns `Alive:false` with no transport error) up to
  `startTimeout` (~30s). No-op when on-demand disabled.
- A single reaper goroutine (started in `New` when on-demand enabled) ticks
  every minute: for each sidecar where `now - lastActive > idleGrace` (10 min)
  AND the container is `Running`, call `dockerctl.Stop`. A churning account
  (retrying StartWatch every few sec) keeps `lastActive` fresh and is never
  stopped — only genuinely idle accounts are reaped.

This is a deliberate simplification of the spec's earlier `MarkActive/MarkIdle`
idea: the heartbeat timestamp already encodes "is this account watching," so no
watcher edits are needed.

## Data flow

```
watcher discovery (HTTP) ── watchable? ──► StartWatch ─► EnsureSidecarUp: docker start + readiness ─► gRPC StartWatch ─► IVS plays
                                           Heartbeat (~60s) ─► bump lastActive
                          └─ nothing ────► idle in pick_campaign (no Start/Heartbeat) ─► lastActive goes stale

reaper (1/min): now-lastActive > 10min && container running ──► docker stop ──► RAM freed
```

## Error handling / graceful degrade

- `dockerctl.New` fails (no socket / no perms): log once at startup, disable
  on-demand control, leave sidecars always-on (today's behavior). Mining
  unaffected.
- `docker start` fails or readiness times out: surface as a normal `StartWatch`
  error → existing watcher backoff. Reaper will retry start on the next watch.
- `docker stop` fails: log, retry next reaper tick.
- Reaper never stops a sidecar that is currently `watching` (guarded by
  `idleSince` only being set when the account has nothing watchable).

## Deployment

- `compose.yml`: rename `grubdrops-browser` → `grubdrops-browser-ttik3r`,
  `grubdrops-browser2` → `grubdrops-browser-phluses` (same image, same
  `expose: 9090`). Add `/var/run/docker.sock:/var/run/docker.sock:ro`? — NO:
  start/stop needs write, mount read-write. `grubdrops` gets the socket mount.
- `.env`: no new per-account vars needed — names auto-derive (`grubdrops-browser-ttik3r`,
  `grubdrops-browser-phluses`). Optionally set `GRUB_KICK_SIDECAR_TEMPLATE` to
  override the default. `GRUB_BROWSER_URLS` can be dropped.
- `depends_on` updated to the renamed services. Sidecars keep
  `restart: unless-stopped`; the daemon's `docker stop` overrides until the
  next `docker start` (a manual `compose up` won't fight it because stop is
  explicit, not a crash).
- Security note: mounting the docker socket grants the daemon root-equivalent
  control of the host's Docker. Accepted for this homelab; documented here.

## Testing

- `dockerctl`: unit tests against a fake Docker client interface (Start/Stop
  idempotent, Running parses state, errors propagate).
- Kick registry: slugify usernames (mixed case, spaces/symbols, empty →
  fallback), derived URL/container name correct, `watchClientFor` pins correctly.
- Lifecycle: with a fake `dockerctl`, assert `EnsureSidecarUp` starts + waits
  for readiness; reaper stops a container whose `lastActive` is older than grace
  AND running; reaper does NOT stop one with fresh `lastActive` or already
  stopped; degrade path when dockerctl is nil (no stop/start, never errors).
- Live (manual, prod): expire/lull → both containers `docker stop` after 10
  min; a live channel reappears → container starts, player plays, progress
  advances; RAM drops to ~daemon-only between events.

## Rollout

Build on the `agent-a12f334b3f499cfa9` worktree (deployed source). Deploy via
the standard COPY-only image + `compose up -d --force-recreate`. Verify both
accounts still mine, then force an idle window to confirm stop/start.

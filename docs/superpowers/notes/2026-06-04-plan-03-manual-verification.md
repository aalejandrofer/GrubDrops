# Plan 3 — Real Twitch Backend manual verification

This runbook walks through a real-account verification of the Twitch backend. Run it once before declaring Plan 3 shipped. Expect 5–15 minutes plus the time required for a drops campaign to be live.

## Prerequisites

- A throwaway Twitch account you don't mind authorizing the miner against (any active account works; only OAuth scopes for read-only profile info are requested).
- Docker + docker compose locally.
- At least one currently-live Rust Drops campaign on Twitch. Check https://www.twitch.tv/drops/campaigns while signed in to see whether anything is live.

## Steps

### 1. Boot the stack

```bash
cd deploy
mkdir -p data
export MINER_MASTER_KEY="$(go run filippo.io/age/cmd/age-keygen 2>&1 | awk '/AGE-SECRET-KEY/ {print $NF}')"
docker compose up --build -d
sleep 5
```

### 2. First-run admin setup

Open `http://127.0.0.1:8080` in a browser. You should be redirected to `/setup`. Set an admin password (≥ 8 chars), submit. You land on the dashboard.

### 3. Add a Twitch account

1. Click **Accounts** → **+ Add account**.
2. Select **Twitch (drops)** in the platform dropdown.
3. Enter your Twitch login handle (e.g. `mytwitchhandle`).
4. Enter a display name (or leave blank to default to login).
5. Click **Create**.

You should be redirected to `/accounts/<id>/login`, showing a USER CODE and a verification URL.

### 4. Authorize Twitch

1. Open the verification URL (`https://www.twitch.tv/activate`) in another browser tab logged into your throwaway Twitch account.
2. Enter the USER CODE shown in the miner GUI.
3. Confirm authorization on the Twitch page.

Within ~5 seconds the miner GUI status partial should flip from "still waiting…" → "authorized — redirecting" and JavaScript redirects you back to `/accounts`.

### 5. Apply changes and observe

1. On `/accounts`, click **Apply changes (reload watchers)**.
2. Go to the **Dashboard**.
3. Your Twitch account card should appear and progress through states: `pick_campaign` → `pick_stream` (or `sleeping` if no eligible streams cached) → `watching` once a live drops campaign is found.

### 6. Capture log evidence

```bash
docker compose logs miner 2>&1 | grep -E '"event":"(state|progress|claim|error)"' | head -40
```

Expected events:
- Many `state` transitions (idle → pick_campaign → pick_stream → watching ...)
- `progress` events as minutes accumulate (one per heartbeat tick)
- Eventually a `claim` event when a drop completes

### 7. Known limitations (Plan 3)

- The allow-list of drops-enabled channels is NOT populated yet. Until that's wired (a follow-up plan revision), the watcher will reach `sleeping` instead of `watching` because `ListEligibleChannels` returns empty. To work around this for now, mine a known drops-enabled channel manually by:
  1. Visiting any live Rust Drops streamer's channel in your browser (with the same Twitch account).
  2. Letting the player run for ~1 minute to register the channel in your inventory.
  3. The miner's `InventoryProgress` query will then surface the progress and the claim flow exercises end-to-end.
- Discord notifications fire if `MINER_DISCORD_WEBHOOK` is set in the compose env.

### 8. Teardown

```bash
docker compose down
```

## Pass criteria

The verification passes if:

1. `/setup` → `/login` → dashboard flow works.
2. Adding a Twitch account redirects to `/accounts/<id>/login`.
3. Completing the device-code activation flips the status partial to "authorized" and redirects.
4. The dashboard shows the Twitch account in a non-stopped state after **Apply changes**.
5. At least one `progress` or `claim` event lands in the container logs.

## Reporting

If verification fails at any step, capture:
- The failing step number.
- `docker compose logs miner` output around the failure.
- Browser network tab for the failing request (relevant headers + response body).

File these into a GitHub issue or paste into the next plan revision so the gap can be patched.

# Plan 4 — Kick backend manual verification

Verifies the Kick browser-sidecar flow against a real Kick account.

## Prerequisites

- A Kick account already logged in via your browser.
- A live drops-enabled Kick channel for Rust.
- Docker + docker compose.

## Steps

### 1. Boot with the browser profile

```bash
cd deploy
mkdir -p data
export MINER_MASTER_KEY="$(go run filippo.io/age/cmd/age-keygen 2>&1 | awk '/AGE-SECRET-KEY/ {print $NF}')"
export MINER_BROWSER_URL="browser:9090"
docker compose --profile browser up --build -d
sleep 10
```

### 2. Setup admin

Open http://127.0.0.1:8080 → /setup → set admin password → land on dashboard.

### 3. Extract Kick cookies from your browser

1. Visit kick.com in your normal browser (logged in).
2. Open DevTools → Application → Cookies → https://kick.com.
3. Copy values for: `kick_session`, `XSRF-TOKEN`, `cf_clearance`.

### 4. Add a Kick account

1. Accounts → + Add account
2. Select **Kick (drops)** and a login handle.
3. Submit → redirects to `/accounts/<id>/login`.
4. Paste the three cookies and the channel login (e.g. `rust-streamer-name`).
5. Submit. The sidecar validates the session. On success you land on /accounts with a green flash showing your Kick username.

### 5. Apply changes

Click **Apply changes (reload watchers)**. The watcher starts. On the dashboard the Kick account card should progress through states.

### 6. Verify via logs

```bash
docker compose logs miner 2>&1 | grep -E '"event":"(state|progress|claim)"' | head -20
docker compose logs browser 2>&1 | head -20
```

Expect:
- `state` transitions on the miner side
- chromedp activity on the browser side (no obvious errors)

### 7. Known limitations

- `RequiredMinutes` for Kick drops is hard-coded to 120 in `kick.Backend.ListActiveCampaigns`. Until the sidecar surfaces the per-drop threshold, this is a static guess.
- Cookies expire — log back into Kick on your real browser → re-run the login flow if 401s appear in logs.
- One sidecar tab per Kick account. Many accounts → many tabs → growing memory. Restart the browser container periodically.
- The Kick inventory parser reads `window.__NEXT_DATA__` and is brittle to Kick frontend refactors. If `Inventory` returns empty when you know you have drops, re-inspect the page DOM and update `parseInventoryNextData` in `internal/auth/browser/sidecar/kick.go`.

### 8. Teardown

```bash
docker compose --profile browser down
```

## Pass criteria

- /setup → login → dashboard works with browser profile up
- Kick login form accepts cookies and shows green flash with username
- Apply changes reaches a non-stopped Kick state on the dashboard
- Sidecar logs show no panics within 5 minutes

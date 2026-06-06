# Kick Cloudflare / CDP wall — findings

_Local testing 2026-06-06, residential IP that works fine in normal Chrome._

## TL;DR

Kick's API (`/api/v1/user`) returns **403 `{"error":"Request blocked by security policy","reference":"9e4db7e3"}`** for **any CDP-driven Chrome** — headless *and* headed, even with a valid session and the browser's own freshly-earned Cloudflare clearance. The block is at the bot-management layer, not Kick auth (auth failure would be 401). **The sidecar's `VerifyAuth` design — manually fetching `/api/v1/user` — is the wrong approach for Kick.**

## What was tested

Tool: `cmd/kick-login` (headed diagnostic). Live cookies kept out of repo at `/tmp/dropsminer-kick-cookies.json`.

| # | Client | Cookies injected | CF page challenge | `/api/v1/user` |
|---|--------|------------------|-------------------|----------------|
| 1 | bare `curl` | session (+cf_clearance) | n/a | **403** |
| 2 | chromedp **headless** | session + cf_clearance | — | **403** |
| 3 | chromedp **headed real Chrome** | session only (`kick_session`+`XSRF-TOKEN`) | **PASSED — earned own `cf_clearance` + `__cf_bm`** | **403** |
| — | normal human Chrome | (its own) | passes | **200, logged in** |

Same `reference: 9e4db7e3` across every failing attempt → a static WAF rule, not a per-request trace.

## What each result rules out

- **curl 403 (even with cf_clearance):** Cloudflare blocks at the TLS/HTTP2 fingerprint layer before checking cookies. A `cf_clearance` token is bound to the fingerprint+IP that earned it; replaying it from curl is rejected. → curl can never validate Kick.
- **Headed real Chrome earned its own `cf_clearance` + `__cf_bm`** but the API fetch still 403'd. This **refutes** "headless detection" and "foreign-clearance poisoning" as the cause. The page-level CF challenge is passed; only the authenticated API fetch is blocked.
- Earlier observation: headed chromedp renders kick.com fine and the login form is interactive — only the login **POST** 403'd (Turnstile action token). So GETs/navigation are fine; the wall is specific to authenticated API calls.

## Root cause (working conclusion)

**CDP attachment is the tell.** The bot sensor (Cloudflare + likely PerimeterX) detects the DevTools protocol connection, flags the session as automation, and the WAF 403s authenticated XHR/fetch API calls — while still serving page HTML. This applies to **all CDP-based automation**: chromedp, Puppeteer, Playwright, Selenium-CDP. **noVNC into a chromedp-driven Chrome does NOT help** — same CDP, same wall.

Not fully ruled out by local test alone: `/api/v1/user` may also require an `Authorization: Bearer <session_token>` header (the `session_token` cookie has Laravel-Sanctum `id|token` shape). A DevTools capture of the real SPA request would confirm, but the cross-session strategy note already commits to the page-scrape path below.

## Path forward

**Do NOT call the Kick API under automation.** Viable approaches:

1. **Navigate-page + scrape embedded state (chosen).** Load the real page in the browser, read state Kick embeds in the DOM / `__NEXT_DATA__` / hydration JSON. Page loads aren't blocked — only API fetches are. Channel discovery = scrape the directory/drops page, not an API. (`ScrapeActiveDrops` already does DOM scraping — that's the pattern to lean on; retire the API-fetch verify.)
2. **Verify auth by DOM**, not API — check whether the logged-in username/avatar renders, instead of fetching `/api/v1/user`.
3. **Watch passively** — keep the stream tab open and let Kick's own JS report watch time, rather than driving APIs. (HyperBeats reportedly works this way.)
4. **Pure-HTTP replay (fallback)** — Go `utls` / curl-impersonate for a real browser TLS fingerprint + replay the SPA's exact headers. No JS sensor runs, so nothing flags CDP — but risk is an API-side PerimeterX token requirement.

## UPDATE 2026-06-06 (round 2) — CDP poisons even MANUAL login

Re-ran `cmd/kick-login` (headed real Chrome, inject only kick_session+XSRF):
- `[1]` browser **earned its own cf_clearance + __cf_bm** (page CF challenge passes).
- `[2]` DOM showed **not logged in**; `[3]` directory page had **empty title, no
  `__NEXT_DATA__`** (Kick is a client-rendered SPA — populates via API).
- **Then the user tried to log in MANUALLY in that Go-launched Chrome window and
  was BLOCKED.** So CF/Turnstile blocks even a human typing credentials, as long
  as the browser was launched/attached by chromedp (CDP).

**Decisive conclusion: CDP attachment is detected and poisons ALL
auth/API/interactive actions on Kick — not just programmatic fetches. chromedp /
any CDP automation is a DEAD END for Kick.** Page HTML renders, but login, the
SPA's own data fetches, and our scrapes all fail. There is nothing useful to
scrape because the SPA can't populate behind the block.

### Real options (chromedp ruled out)
1. **Browser extension in the user's REAL Chrome (no CDP).** The user's normal
   Chrome already works on Kick. A WebExtension running there can: read the
   logged-in session, scrape drops/channels from the live DOM/SPA, open watch
   tabs (Kick's own JS reports watch time), and click claim. No CDP, no CF fight.
   Talks to the Go backend over HTTP/WebSocket. Most robust; needs the user to
   run the extension.
2. **Pure-HTTP replay with TLS-fingerprint impersonation** (Go utls /
   curl-impersonate). No browser, no CDP → nothing flags automation. Needs: a
   valid kick_session + a browser-identical TLS (JA3) fingerprint + the SPA's
   exact request headers. Risk: cf_clearance is IP+fingerprint bound, and an
   API-side PerimeterX/Sanctum Bearer token may be required. Worth a spike: try
   curl-impersonate from the user's IP with their session.
3. Give up auto-watch on Kick; keep manual channel entry (status quo).

Recommendation: **spike option 2** (utls/curl-impersonate, quick to test from the
user's machine) to see if non-CDP HTTP works at all; if it 403s too, go **option 1**
(extension). Do NOT invest more in chromedp for Kick.

## Code touched

- `cmd/kick-login/main.go` — headed local diagnostic. Reuses `sidecar.StealthScript`. Dumps the cookies the browser earned + the `/api/v1/user` status. Repurpose or delete once the page-scrape path lands.

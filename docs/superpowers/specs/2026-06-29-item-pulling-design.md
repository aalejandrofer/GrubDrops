# Better item (benefit) pulling on /drops — design

**Date:** 2026-06-29
**Status:** approved (brainstorm), pending implementation plan
**Base:** branches off `master` after the manual-mark-collected merge (`0489f57`), which already broadened `sessionForPlatform` to fall back to any account's session.

## Problem

The `/drops` campaign-items panel often fails to show a campaign's items:

1. **Stuck "Loading…"** — e.g. Twitch Minecraft "Tubbo's WatchTime". The campaign has a scrape-fallback **synth id** (`Minecraft|Tubbo's WatchTime Sun, May 31, 4:00 PM UTC - ...`) containing spaces and `|`. The items request `GET /drops/campaigns/<id>/items` 404s (the handler returns `http.NotFound` when the lookup fails / the URL is malformed), and because HTMX does not swap on non-2xx, the placeholder "Loading items…" stays forever.
2. **"No items recorded"** on sub/action campaigns — e.g. "MSI - Sub Drop". `fetchDetails` skips any drop with `RequiredMinutesWatched <= 0`, so sub-gift / action-only drops yield zero benefits.
3. **"No items"** on Kick (and some non-whitelisted Twitch) — lazy-fetch only resolves for backends implementing `CampaignDetailer`; the Kick backend does not, so its empty panels never backfill.
4. **Missing item icon** — synth scrape benefits never get an `image_url` (only ~2 rows affected in prod).

## Goals

- The items panel **always resolves** (never hangs), for any campaign id.
- Sub/action (0-minute) drops are **visible** as items (rendered dim), without the watcher trying to mine them.
- Kick campaign panels backfill their items on open.
- The icon path is correct where a source exists; honest placeholder where none does.

## Non-goals / honest limits

- **Synth scrape benefits have no image source** — the sidecar HTML scrape does not capture an image URL. We will not fabricate one; the placeholder remains. (#4 is therefore "ensure the GQL path maps the image field" + "don't hang/err" — not "every item gets an icon".)
- We are not rewriting the sidecar scrape or changing how synth campaigns are discovered/mined beyond what these fixes require.

## Risk + rollout

#1 and the icon mapping are web-layer / low risk. **#2 and #3 touch Twitch/Kick discovery (the accrual-adjacent zone).** Per project rules, the discovery/watcher changes must be live-drop verified before tagging, and the watcher pick-filter change gets a unit test. The items-endpoint and template changes are covered by unit/render tests (the default gate).

## Existing mechanics (reference, file:line)

- **Discovery persists benefits** from `c.Benefits`: `internal/store/campaign_persister.go:126-139` (`UpsertBenefit` per benefit; skips empty-id).
- **Twitch skips detail fetch for non-whitelisted games**: `internal/platform/twitch/campaigns.go:214,248-250` (`shouldFetchDetails := sess.GameFilter == nil || sess.GameFilter(c.Game.DisplayName)`), so non-whitelisted campaigns persist with `Benefits=[]`.
- **Synth id + synth benefit**: `internal/platform/twitch/campaigns.go:228` (`linkChecked := !strings.ContainsAny(c.ID, "| ")`), `:259-268` (when `ContainsAny(c.ID,"| ")` and kind != "reward", synthesize one benefit `c.ID+"_default"`, `RequiredMinutes:5`, no image).
- **fetchDetails skips 0-min drops**: `internal/platform/twitch/campaigns.go:364-368` (`if td.RequiredMinutesWatched <= 0 { continue }`).
- **fetchDetails maps image**: `internal/platform/twitch/campaigns.go:375-385` (`ImageURL: be.Benefit.ImageAssetURL`).
- **CampaignDetails rejects synth ids**: `internal/platform/twitch/backend.go:61-76` (`if strings.ContainsAny(campaignID, "| ") { return nil, nil }`).
- **lazyFetchBenefits**: `internal/api/handlers_drops.go:34-78` (type-asserts `CampaignDetailer`; returns false on no detailer / no session / err / zero benefits).
- **items handler**: `internal/api/handlers_drops.go:1320-1382` — `renderCampaignItems` does `GetCampaign(id)` → `http.NotFound` on error; then `ListBenefitsForCampaign`, lazy-fetch if empty; renders `drops_campaign_items`.
- **items route**: `internal/api/server.go:354` `authed.Get("/drops/campaigns/{id}/items", dropsH.items)`; `items` reads `chi.URLParam(r, "id")`.
- **Kick backend** does NOT implement `platform.CampaignDetailer`. Kick campaign detail parsing lives in `internal/platform/kick/api.go:193-199` (rewards → benefits, with `ImageURL: absImageURL(...)`).
- **template**: `internal/web/templates/_drops_campaign_items.html:2` (`{{if .Benefits}}…{{else}} no_items {{end}}`), `:12-14` (icon: `{{if .ImageURL}}<img>{{else}}<span class="ph">`).
- **outer row** issues the request: `_drops_table.html` rows use `hx-get="/drops/campaigns/{{.CampaignID}}/items"`.

## Design

### Component 1 — Items panel never hangs (web-layer, low risk)

**1a. Never return non-2xx from the items endpoint.** In `renderCampaignItems`, when `GetCampaign(id)` errors, do NOT `http.NotFound`. Instead render the `drops_campaign_items` partial with zero benefits and a distinct "couldn't load this campaign" state (200 OK). HTMX always swaps, so the placeholder is always replaced — the panel can never get stuck on "Loading…", for any id, including ids with `/` that can't route cleanly.

Add a `campaignDetailRow.LoadError bool` (or reuse the empty-benefits path with a flag) so the template can show "Couldn't load items for this campaign." vs the normal "No items recorded yet." New i18n key `campaign_items.load_error` (en/es/zh-CN).

**1b. Correctly encode the id in the request URL.** Add a template func `pathEscape` (wrapping `url.PathEscape`) and use it in the items `hx-get` URL in `_drops_table.html` (and any other row that links to the items endpoint): `hx-get="/drops/campaigns/{{pathEscape .CampaignID}}/items"`. This percent-encodes spaces/`|` so synth ids resolve and match the stored id after chi decodes the path param. (Ids containing `/` still cannot round-trip through path routing — 1a covers those by rendering a clean error instead of hanging.)

Register `pathEscape` wherever the template `FuncMap` is built (same place `t` "translate" is registered).

### Component 2 — Show sub/action (0-minute) drops (discovery, medium risk)

**2a. Stop discarding 0-min drops in `fetchDetails`.** Replace the `if td.RequiredMinutesWatched <= 0 { continue }` skip (`campaigns.go:364-368`) with: include the drop as a benefit with `RequiredMinutes: 0` (mapped through the same `BenefitEdges` loop so it keeps name + `ImageURL`). These then persist and the template already renders `RequiredMinutes <= 0` as the dim "action required" type.

**2b. Move the "don't mine 0-min" filter into the watcher's pick step.** The skip previously doubled as mining protection (a 0-min benefit looks instantly satisfied). Add an explicit guard in the watcher's benefit-selection (`pickCampaign`/benefit pick) to ignore benefits with `RequiredMinutes <= 0` when choosing what to mine, so display gets them but the miner never selects/claims an action-only drop. Cover with a watcher unit test: a campaign whose only drops are 0-min is NOT picked for mining (watcher goes idle/next), while a mixed campaign still mines its watch-time tier.

### Component 3 — Kick CampaignDetailer + non-whitelisted backfill (discovery, medium risk)

**3a. Implement `CampaignDetailer` on the Kick backend.** Add `CampaignDetails(ctx, sess, campaignID) ([]platform.DropBenefit, error)` that fetches the single campaign's detail via the existing utls Kick API path and maps its rewards to `DropBenefit` (reusing the `api.go:193-199` reward→benefit mapping, including `absImageURL` for the proxied image). With this, `lazyFetchBenefits` resolves Kick panels on open. Return `(nil, nil)` (not an error) when the campaign genuinely has no rewards, so the panel shows the clean "No items" state, never an error.

**3b. Non-whitelisted Twitch** already backfills via the existing Twitch `CampaignDetailer` + the merged `sessionForPlatform` fallback; Component 2 ensures sub/action drops are included. No extra code beyond #2.

### Component 4 — Icons (web-layer)

The real GQL path already sets `ImageURL` from `be.Benefit.ImageAssetURL` (`campaigns.go:375-385`); Kick sets it from the reward image (`api.go`). Component 2 keeps that mapping for the now-included 0-min drops. **Synth scrape benefits have no image source — the placeholder stays** (honest limit). No fabrication. Confirm the Kick image proxy (`/img/kick?u=`) still wraps Kick benefit images in `renderCampaignItems` (existing behavior, keep).

## Testing

- **Items endpoint never 404s (1a):** unit test — `renderCampaignItems` with an unknown id returns 200 and a partial containing the load-error string, not a 404.
- **pathEscape (1b):** render test — a row whose `CampaignID` has a space/`|` emits an `hx-get` with the id percent-encoded; a normal uuid id is unchanged.
- **0-min drops included (2a):** Twitch `fetchDetails`/parse test — a campaign with a 0-min drop yields a benefit with `RequiredMinutes == 0` (was previously dropped).
- **Watcher ignores 0-min for mining (2b):** watcher unit test (reuse the test harness) — a campaign whose only benefits are 0-min is not picked; a mixed campaign still picks its watch tier. Existing prune/mining tests stay green.
- **Kick detailer (3a):** unit test — Kick `CampaignDetails` maps a sample rewards payload to benefits (name, required_minutes, proxied image); empty payload returns `(nil,nil)`.
- **Template (4):** existing render test — benefit with empty `ImageURL` renders the `.ph` placeholder; with a URL renders `<img>`.
- i18n parity stays green (new `campaign_items.load_error` key in all three locales).

## Rollout gate

Tag only after: green build + unit tests (covers 1, 4, and the watcher pick filter), AND **live-drop verification** of the discovery changes (#2 Twitch sub/action drop visible + not mined; #3 Kick panel backfills) per the project's accrual/discovery gate. Log all changes under `docs/CHANGELOG.md` `[Unreleased]`.

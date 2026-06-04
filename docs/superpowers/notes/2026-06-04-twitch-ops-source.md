# Twitch operations source snapshot

Captured 2026-06-04 from DevilXD/TwitchDropsMiner@c5e6286c41dab46e1189333eede734e3b1995dc4 (master branch).

## Source files inspected

- `constants.py` — https://raw.githubusercontent.com/DevilXD/TwitchDropsMiner/master/constants.py
- `twitch.py` — https://raw.githubusercontent.com/DevilXD/TwitchDropsMiner/master/twitch.py
- `channel.py` — https://raw.githubusercontent.com/DevilXD/TwitchDropsMiner/master/channel.py

## Constants

- CLIENT_ID: `kimne78kx3ncx6brgo4mv6wki5h1ko` (ClientType.WEB in constants.py)
- USER_AGENT: `Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/138.0.0.0 Safari/537.36` (ClientType.WEB, pinned Chrome 138 desktop UA)
- GraphQL endpoint: `https://gql.twitch.tv/gql` (twitch.py line ~1298)
- Device authorize: `https://id.twitch.tv/oauth2/device` (twitch.py, `_oauth_login` method)
- Token: `https://id.twitch.tv/oauth2/token` (twitch.py, `_oauth_login` method)
- Minute-watched: **no static URL** — see section below

## Persisted query hashes

| Operation (DevilXD key) | operationName (on wire) | sha256Hash | Source |
|---|---|---|---|
| `GetStreamInfo` | `VideoPlayerStreamInfoOverlayChannel` | `198492e0857f6aedead9665c81c5a06d67b25b58034649687124083ff288597d` | constants.py:308 |
| `ClaimDrop` | `DropsPage_ClaimDropRewards` | `a455deea71bdc9015b78eb49f4acfbce8baa7ccbedd28e549bb025bd0f751930` | constants.py:327 |
| `Inventory` | `Inventory` | `d86775d0ef16a63a33ad52e80eaff963b2d5b72fada7c991504a57496e1d8e4b` | constants.py:345 |
| `Campaigns` | `ViewerDropsDashboard` | `5a4da2ab3d5b47c9f9ce864e727b2cb346af1e3ea8b897fe8f704a97ff017619` | constants.py:362 |
| `CampaignDetails` | `DropCampaignDetails` | `039277bf98f3130929262cc7c6efd9c141ca3749cb6dca442fc8ead9a53f77c1` | constants.py:370 |
| `PlaybackAccessToken` | `PlaybackAccessToken` | `ed230aa1e33e07eebb8928504583da78a5173989fadfb1ac94be06a04f3cdbe9` | constants.py:387 |
| `AvailableDrops` | `DropsHighlightService_AvailableDrops` | `782dad0f032942260171d2d80a654f88bdd0c5a9dddc392e9bc92218a0f42d20` | constants.py:379 |
| `CurrentDrop` | `DropCurrentSessionContext` | `4d06b702d25d652afb9ef835d2a550031f1cf762b193523a92166f40ea3d142b` | constants.py:353 |

## Minute-watched heartbeat shape

DevilXD as of this commit does NOT use a static spade/countess URL. The `send_watch()` method (channel.py) posts a **GQL mutation** to `https://gql.twitch.tv/gql`:

```
mutation SendEvents($input: SendSpadeEventsInput!) {
  sendSpadeEvents(input: $input) {
    statusCode
  }
}
```

The mutation variables contain a `data` field that is **base64(gzip(json_minify(payload)))** with `encoding: "GZIP_B64"` and `repository: "twilight"`. The inner JSON payload is a list with one event object:

```json
[
  {
    "event": "minute-watched",
    "properties": {
      "broadcast_id": "<str>",
      "channel_id": "<str>",
      "channel": "<login>",
      "client_time": "<ISO8601>",
      "game": "<name or empty>",
      "game_id": "<id or empty>",
      "hidden": false,
      "is_live": true,
      "live": true,
      "logged_in": true,
      "minutes_logged": 1,
      "muted": false,
      "user_id": <int>
    }
  }
]
```

Encoding: `data = base64(gzip(json_minify(payload)))`, sent as `variables.input.data` with `variables.input.repository = "twilight"` and `variables.input.encoding = "GZIP_B64"`. A successful response has `statusCode: 204`.

Note: DevilXD still has a legacy `_send_watch_spade()` method that uses a dynamically-scraped `spade_url` from the streamer's HTML page, but this is marked `# NOTE: This is currently unused.`. The active path is `send_watch()` which calls the GQL mutation.

## Notes / anomalies

1. **No `DropsPage_ContentList`** — the plan spec mentions `DropsPage_ContentList` but DevilXD does not use this operation. The equivalent is `Campaigns` / `ViewerDropsDashboard` which lists all available drop campaigns. Our Go code uses `OpCampaigns` for this.

2. **No `WithIsStreamLiveQuery`** — the plan spec mentions this but DevilXD's stream liveness check is `GetStreamInfo` / `VideoPlayerStreamInfoOverlayChannel`. Our Go code uses `OpGetStreamInfo` for this.

3. **No static `minuteWatchedURL`** — the heartbeat is sent as a GQL mutation to `https://gql.twitch.tv/gql`, not to a dedicated analytics endpoint. The `minuteWatchedURL` constant in ops.go is left as the GQL endpoint as that is what is actually used.

4. **`ClaimDrop` key vs wire name** — DevilXD's dict key is `"ClaimDrop"` but the on-wire `operationName` is `"DropsPage_ClaimDropRewards"`. We expose this as `OpClaimDrop` with `Name: "DropsPage_ClaimDropRewards"`.

5. **Chrome 138** — USER_AGENT is pinned to Chrome 138.0.0.0 as of commit c5e6286. This will need updating when Twitch starts rejecting older browser strings.

6. **Auth header format** — DevilXD sends `Authorization: OAuth <token>` (not `Bearer`). See twitch.py headers method.

7. **`CurrentDrop` / `DropCurrentSessionContext`** — useful for polling in-progress drop state per channel. Included in ops.go as `OpCurrentDrop`.

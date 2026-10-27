# Force watch (GRUB-24)

## Intent

When an account has nothing to mine (empty plan queue), it should still watch
something useful: a channel the user picked. That earns watch time and Twitch
channel points instead of sitting idle. A drop always wins: the moment the plan
has something to mine, the account leaves force watch. If the forced streamer
has a drop the account can claim, claim it.

1.x had this as a per-account toggle plus a per-account ranked channel list.
v2 dropped it. This brings it back as one shared, ordered channel list on the
Games page.

## Goals

- A "Force watch" panel on the Games page, in the left rail under Lists.
- Add a channel by pasting its link. Platform comes from the host
  (`twitch.tv`, `kick.com`; `www.` and `m.` allowed). Name must match
  `^[a-z0-9_]{2,25}$` after lowercasing.
- Each channel has a set of accounts on the same platform that force-watch it.
- **Panel order decides.** An idle account watches the first channel in the
  list that it is assigned to and that is live.
- The red dot on each row shows whether the channel is live now, for every row,
  not only the one being watched.
- Account names on a row show in green when that account is watching it now.
- Works on Twitch and Kick.
- Claim drops earned on the forced channel.

## Non-goals

- Clicking Twitch bonus chests / channel-point claims. Watching only.
- Per-account ordering. Order is the panel's, shared by all accounts.
- Mining Kick campaigns with no game (GRUB-23, parked; force watch covers it).
- Drag and drop. Reorder is up/down buttons.

## Anti-goals

- Force watch must never delay or block drop mining. If a plan target exists,
  the account mines it.
- Must not change Twitch beacon cadence (`runner.DefaultTick`, 60s) or add a
  `Client-Integrity` header.
- Must not add log churn: no INFO line per tick or per live check.

## UI (decided, from sandbox `force-watch`, variant A)

Mockup: `web/src/routes/sandbox/force-watch/` variant `a`.

- Panel header "FORCE WATCH" with a purple `+` button top right (no "Add
  channel" button at the bottom). Hint under the header: "Watched when no
  drops."
- Row: rank number, platform mark, channel name, live dot (red when live, grey
  when offline), assigned account names joined by " · " (green = watching now).
  Tools: up, down, edit, remove.
- Remove asks inline first: "Remove {name}?" with Remove / Cancel, same as the
  Games page game remove.
- `+` opens a dialog "Add channel":
  - field labelled "Channel link (twitch.tv / kick.com)", placeholder
    `https://www.twitch.tv/keshaeuw`;
  - once it parses, show platform mark + name + "Twitch channel"/"Kick channel";
  - errors: not a twitch.tv/kick.com channel link; already in the list;
  - "Accounts that watch it when idle": chips for accounts of that platform only,
    **none selected by default**; "No Twitch/Kick accounts yet." when empty;
  - Cancel / Add. Add disabled until at least one account is picked.
- Edit reopens the dialog titled with the channel name, account chips only,
  Save.
- Empty list: "No channels yet."
- All strings go through i18n (en, es, zh-CN).
- Phone width: rail stacks above main, tools always visible.

## Home tile (decided, from sandbox `home-force-watch`, variant A)

Mockup: `web/src/routes/sandbox/home-force-watch/` variant `a`.

- A "Force watch" KPI tile in the **third** slot of the Home KPI row: Minutes
  watched, Claimed today, Force watch, Accounts healthy. Slightly wider than
  the others.
- Shown only when the Force watch list has at least one channel. Otherwise the
  row stays at three tiles as today.
- Lists **live channels only**, one line each: red dot, platform mark, channel
  name, then on the right the accounts watching it now in green, or "Nobody
  watching".
- **At most two lines.** Channels an account is watching come first (panel
  order among them); remaining slots go to the next live channels in panel
  order. More live channels than that: one line under them, "{n} more live".
- When channels exist but none is live: "None live" with "{n} channels
  waiting" under it.
- The whole tile links to `/games`.
- Updates live with the rest of Home (same SSE refresh).
- Phone width: KPI grid goes 2 x 2.

## Behaviour

### Store

New migration (next number after `0022`):

- `force_channels(id INTEGER PRIMARY KEY, platform TEXT NOT NULL, channel TEXT
  NOT NULL, rank INTEGER NOT NULL, created_at INTEGER NOT NULL,
  UNIQUE(platform, channel))`
- `force_channel_accounts(channel_id INTEGER NOT NULL REFERENCES
  force_channels(id) ON DELETE CASCADE, account_id TEXT NOT NULL REFERENCES
  accounts(id) ON DELETE CASCADE, PRIMARY KEY(channel_id, account_id))`
- Carry over 1.x: for each account whose kv `force_watch:<id>` is `"1"`, copy
  its `account_force_channels` rows (platform = the account's platform),
  merging duplicates by (platform, channel), ordered by (rank, created_at). Leave
  the 1.x table and kv rows in place. (Prod has none; this is for other
  installs.)
- A store wrapper with `List(ctx)` and a transactional `Set(ctx, []Channel)`
  that replaces the whole list. Assigning an account on a different platform
  is rejected.

### API

- `GET /api/force-watch` returns the ordered list:
  `[{platform, channel, live, accounts: [account_id]}]`. `live` is
  `true`/`false`, or `null` when not checked yet.
- `PUT /api/force-watch` replaces the whole list (order = rank). Validates
  platform, channel name, account existence and platform match, no duplicates.
  CSRF like other writes. Reloads affected runners and publishes an SSE event
  so the panel and account states refresh.

### Live checks

- One shared checker in the daemon looks up a channel by platform + login and
  returns `platform.Stream` plus live. Cached per channel for about 2 minutes;
  concurrent callers share one request.
- Twitch: single-login `OpGetStreamInfo` (same query `listEligible` uses), with
  no game filter. Kick: `ChannelLivestream` / `channelAndLivestream` over the
  utls client. Both platforms need an exported single-channel lookup on the
  backend.
- While the list is non-empty, the checker refreshes all channels in the
  background on the same cadence so every row's dot is current. `GET` reads the
  cache and never blocks on the network for long.

### Runner

- `runner.Config` gets a live func returning this account's ordered force
  channels (like `Priority`/`ScanEvery`), plus access to the checker.
- `pick`: when the plan queue is empty (sleeping or awaiting connect), walk the
  account's force channels in order and start watching the first live one. New
  state `force_watching`. If none is live, sleep as today, with the 5-minute
  recheck so a channel going live is picked up.
- An account with no chosen games but force channels must still force watch
  (today `pick` stops it as `no_games`).
- While force watching:
  - heartbeat on the normal 60s tick (Twitch Spade beacon, Kick WS / IVS
    sidecar as today);
  - about every 5 minutes, rescan the plan. A non-empty queue stops the force
    watch and goes to picking (drop wins);
  - same check: if the channel went offline, or a higher-ranked assigned channel
    is live, switch;
  - PubSub stream-down on the forced channel switches at once;
  - claim: run the Kick completed-reward sweep as during normal watching, and on
    Twitch claim any claimable drop in the inventory, even if its game is not
    in the chosen list.
- A Rescan, Mine now, or list change ends force watch and re-picks.
- Snapshot reports state `force_watching` and the channel, so Home and the
  Games panel can show it. Add the state's label to every locale.
- Logs: one INFO line when force watch starts or switches channel, DEBUG for
  checks.

## Release gate

This touches the watch path and claims, so it needs a live check before any tag:
an idle Twitch account accrues watch time on a forced channel, and leaves it
when a drop becomes available. CHANGELOG `[Unreleased]` gets an Added entry.

## Left to the builder

- Exact names of the checker, store wrapper, SSE event and state.
- How the runner reuses `start`/`tick`/`stopWatch` without a plan target
  (today `tick` needs `current`).
- How the Games page learns "watching now" (account snapshots vs a field on the
  force-watch response).

# Round 2 brief

Read `BRIEF.md` first: everything there still applies (audience, look, tokens, rules, sample data), except where this file overrides it.

## Feedback from round 1

Jandro liked **09 Pulse** (its sidebar shell, KPI numbers, now-mining list) and **08 Game cards** (game-first view, per-game progress across accounts). **03 Drop board** could be a hit, but it reads like a drag-and-drop board everywhere, which is wrong. Rules for 03 from now on:

- **Mining** always shows every added account and its status (including `grub_alt` signed out). Not draggable.
- **Up next** is the only reorderable part. Reordering it changes the mining order temporarily (this session). Permanent order is the Games page priority; say so in one short line, with a "Reset to Games order" action.
- **Claimed** shows the last 10 claims only.
- It must not look like a full kanban board. Sections/lists with clear roles, not three identical card columns.

## Extra sample data (round 2)

Up next queue (default order, from Games priority):
1. Rust: "Rust Twitch Drops Round 41", drop 2 "Road Sign Kilt" (60 min), grubfan, after Hazmat Suit Skin
2. Rust: drop 3 "Tempered AK" (120 min), grubfan
3. Marvel Rivals: "Season 4 Twitch Drops", "Spray: Grub Sighting" (30 min), first free account
4. Marvel Rivals: "Nameplate: Hive" (60 min)

Last 10 claims (newest first): Rust Wood Box Skin (today 09:12, grubfan), Marvel Rivals Spray (today 07:40, grubfan), Kick Rust Hoodie (yesterday 22:05, grubfan_kick), Rust Garage Door Skin (yesterday 18:30, grubfan), Marvel Rivals Banner (Oct 23, grubfan), Kick Rust Bandana (Oct 23, grubfan_kick), Overwatch Spray "Grub" (Oct 21, grubfan), Overwatch Player Icon (Oct 21, grub_alt), Rust Sleeping Bag Skin (Oct 20, grubfan), Kick Rust Beanie (Oct 19, grubfan_kick). 27 all time. Use "2 claimed today" (fixes round 1's mismatch).

Games page data (priority order, plus the add flow):
- Rust: Twitch campaign "Round 41" (3 drops, ends Oct 31) and Kick campaign "Kick x Rust" (2 drops, ends Nov 2). Mining.
- Marvel Rivals: Twitch "Season 4 Twitch Drops" (2 drops, ends Nov 8). Waiting.
- Overwatch: no campaign live. Last seen Oct 21.
- Add game: search box; typing "dead" suggests "Deadlock" and "Dead by Daylight". A game with no known campaign can still be added ("We'll watch for campaigns"). Hint: each extra game adds a little scan time; show "3 games · last scan 41 s".
- Priority change here is permanent.

Rewards page: in-progress drops (with progress), then a claimed history (all 27, show ~10 rows and a "Load more"), filter chips by game and account.

## Pages and paths

Each direction gets three pages in one folder, linked to each other through its own nav:
- `index.html` (Home), `games.html`, `rewards.html`
- Settings can be a `#` link.

Folders:
- `r2-08-game-cards/` — direction 08, evolved
- `r2-09-pulse/` — direction 09, evolved
- `r2-03-drop-flow/` — direction 03, rebuilt per the rules above. Pick its nav (sidebar or top bar).

Combined Home screens (Home `index.html` only; nav links to Games/Rewards can be `#`):
- `r2-h1-pulse-games/` — 09 shell (sidebar, status pill, 3 KPIs) with 08's game-first cards replacing the now-mining list.
- `r2-h2-pulse-flow/` — 09 shell and KPIs, then 03's three sections: Mining (all accounts), Up next (reorderable, temporary), Claimed (last 10).
- `r2-h3-games-queue/` — 08's top bar and game cards, with 03's Up next queue as a side column (stacks below on phones) and a slim claimed strip.

Start from the round 1 file of the same direction (`08-game-cards/`, `09-kpi-strip/`, `03-kanban-flow/`) and keep its visual language unless the feedback says otherwise. Up next drag can be faked with up/down buttons plus drag handles and a tiny inline script. The mock tag reads e.g. `r2 · 09 Pulse · Games`.

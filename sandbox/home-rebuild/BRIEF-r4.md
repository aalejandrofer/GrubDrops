# Round 4 brief

Read `BRIEF.md` and `BRIEF-r2.md` for audience, tokens and sample data. Round 3 (`r3-h3/`) is the approved direction. Round 4 lives in `r4/` and starts as a copy of `r3-h3/` (index, games, rewards). Never edit `r3-h3/` or earlier folders.

## Changes Jandro approved

1. **One problem signal.** The header chip "⚠ 1 account needs login" is the single fix entry point (links to Settings > Accounts, `settings.html#accounts`). The "Accounts healthy" KPI stays amber (2/3) but drops its "Re-login" link: sub-line reads "grub_alt signed out". The `grub_alt` line inside the Rust card stays muted, no action link.
2. **First KPI = all-time total.** Label "Total Minutes Watched", value `18,240 min`, sub-line `412 today · 3 accounts`.
3. **Real box art** from Twitch's CDN (verified URLs, 285x380 jpg, use `object-fit: cover`):
   - Rust `https://static-cdn.jtvnw.net/ttv-boxart/263490_IGDB-285x380.jpg`
   - Marvel Rivals `https://static-cdn.jtvnw.net/ttv-boxart/1264310518_IGDB-285x380.jpg`
   - Overwatch `https://static-cdn.jtvnw.net/ttv-boxart/515025_IGDB-285x380.jpg`
   - Dead by Daylight `https://static-cdn.jtvnw.net/ttv-boxart/Dead%20by%20Daylight-285x380.jpg`
   - Deadlock: no art available. Show the fallback (the patterned block with the name) so the fallback state is visible.
   A dark overlay/gradient at the art's bottom edge only if text sits on it; keep it subtle.
4. **Game cards shrink by priority.** Add a 4th chosen game: **Dead by Daylight** (priority 4, no campaign live, last seen Oct 12). Card 1 (Rust, mining) is the largest; card 2 smaller; card 3 smaller still; card 4 smallest. Scale art width, padding, title size and detail shown down the list (e.g. #1 full per-account progress, #2 campaign line + queued drops, #3 one line, #4 one compact line). Same rule on the Games page priority list, but gentler (it's an editing list). Update KPIs/headers where game count shows (4 games).
5. **Up next trimmed.** Helper text becomes "Order for this session only". "Reset to Games order" is hidden until the order changes, then appears (and resets when clicked).
6. **Live feel.** Replace "Today · 10:11" with "updated 12 s ago" that counts up every second via a tiny script, and resets to "just now" every ~15 s, with the Rust progress bars and the "18m left" ETA nudging forward slightly on each reset. Subtle, no flashing.
7. **Version popover.** Clicking the version (top right) opens a small popover anchored under it:
   - Latest state (Home): "You're on the latest version, v1.4.2" + "Release notes" link.
   - Update state (Games, Rewards, Settings): "v1.5.0 is available" + 3 short release-note bullets (invent plausible ones: "Telegram notifications", "Faster game scan", "Fix: Kick claims retry after timeout") + a copyable command block `docker compose pull && docker compose up -d` with a Copy button (copies to clipboard, shows "Copied") + "Full release notes" link. Closes on outside click and Escape.
8. **Contrast and size floor.** Raise the dimmest text colours so text passes WCAG AA on its background, and make the smallest text at least 12px (mono labels included). Keep the calm look.

## Settings page

Separate brief section for `r4/settings.html`, same shell, pill-free header ("Settings" + status + chip). Settings has a left sub-nav within the page: **Accounts, Notifications, Health, Updates, General**. Show all sections stacked on one page with anchors (`#accounts` etc.), sub-nav highlights the current section on scroll.
- **Accounts:** list of the 3 accounts (avatar initials, platform mark, status: mining / signed out). `grub_alt` row is the problem: red-ish "Signed out" + primary "Log in again" button (this is where the header chip lands). "Add account" button with a small Twitch/Kick choice. Each row has a quiet "Remove" in an overflow menu.
- **Notifications:** Discord webhook (filled, masked `https://discord.com/api/webhooks/•••• ` + "Send test"), Telegram (bot token + chat id, empty, "Coming in v1.5.0" note), and toggles for what to notify: Drop claimed, Account needs login, Errors, Daily summary.
- **Health:** the live event log moved here. Mono log list, newest first, ~12 entries with time, level dot, account, message (e.g. "10:24 grubfan  claimed progress 42/60 Hazmat Suit Skin", "10:03 grub_alt  session expired, mining stopped", "09:12 grubfan  claimed Rust Wood Box Skin", Kick sidecar started/stopped, scan finished 41 s · 4 games). Filter chips: All / Problems / Claims. Plus a 3-item status row: Miner running 6d 4h, Kick sidecar: 1 running, Last scan 41 s ago.
- **Updates:** current version v1.4.2, latest v1.5.0, "Check for updates" button, auto-check toggle, the same docker command block.
- **General:** language select (English, Español, 简体中文), time format 24h/12h, theme (Dark default; "Light" option shown).

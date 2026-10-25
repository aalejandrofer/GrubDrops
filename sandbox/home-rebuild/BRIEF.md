# Home rebuild mockups: shared brief

GrubDrops is being rebuilt. These are 10 throwaway static HTML mockups of the **Home screen only**, each a different style, so Jandro can pick a direction.

## Who and why

- Users: public self-hosters (strangers from GitHub/Discord, running it on a NAS or VPS).
- Habit: set and forget. They open it after setup only when a notification says something is wrong or a drop was claimed.
- Home must answer one question within seconds: **"Is it working?"**. Less information, fewer buttons, direct.
- App mode: "choose your games". Users pick games; GrubDrops mines only those.
- App pages (nav): **Home, Games, Rewards, Settings**. Accounts live inside Settings. The live event log moves to Settings > Health; do not put a full event log on Home.

## Look

- Inspiration: Linear / Kaneo. Compact, calm, minimal chrome, subtle borders.
- Colours: GitHub-style greyish dark. Use the tokens in `../_assets/base.css` (link it: `<link rel="stylesheet" href="../_assets/base.css">`). Purple (`--accent`) is the brand accent. Twitch/Kick colours only as small platform marks.
- Logo: `../_assets/logo.png` (the grub mascot, 512x512 PNG). Keep it small in nav.
- Clean slate otherwise. No resemblance to the current UI is needed.
- Quality bar: looks designed, not AI-template. No gradient-blob heroes, no emoji icons, no glassmorphism everywhere, no generic 3-card feature rows. Inline SVG icons (simple stroke icons) only. Restraint and good type hierarchy over decoration.
- Must not horizontally scroll at 390px phone width; use a 16px side gutter on mobile.

## Sample data (use exactly this, so mockups are comparable)

Accounts (3):
1. `grubfan` (Twitch): mining **Rust** "Rust Twitch Drops Round 41", drop "Hazmat Suit Skin", on channel `hJune`, 42 / 60 min, ~18 min left. Then 2 more drops queued in that campaign.
2. `grubfan_kick` (Kick): mining **Rust**, drop "Kick Garage Door", on channel `posty`, 95 / 120 min, ~25 min left.
3. `grub_alt` (Twitch): **problem**: session expired, needs re-login (Settings > Accounts). Not mining.

Chosen games (priority order): Rust, Marvel Rivals, Overwatch.
- Marvel Rivals: campaign live, waiting (accounts busy with Rust).
- Overwatch: no campaign live right now.

Rewards: 3 claimed today ("Rust Wood Box Skin" 09:12, "Marvel Rivals Spray" 07:40, "Kick Rust Hoodie" yesterday 22:05). 27 claimed all time.

## Per-mockup variables

Each mockup has its own navigation style, problem treatment (how the `grub_alt` re-login problem shows) and account display. The list below says which. Write a small fixed tag in the bottom-right: `<div class="mock-tag">NN · Style name · <a href="../">all mockups</a></div>`.

## Rules

- One self-contained `index.html` per mockup (inline `<style>`, optional tiny inline `<script>` for hover/toggle). No external JS libraries. Google Fonts only through base.css.
- `<title>`: `GrubDrops · NN Style name`.
- Static; no real data fetching. Links can be `#`.
- Use only the sample data above. Copy in plain English, short.

## The 10 styles

01. **status-hero** — "Status hero". Top bar nav (logo + 4 tabs). One huge plain-language status sentence ("Mining Rust on 2 accounts. Next drop in 18 min.") with a single combined progress visual. Accounts combined into one line ("2 of 3 accounts mining"). Problem: a single top banner with one "Fix" button.
02. **linear-sidebar** — "Linear sidebar". Slim left sidebar (Linear-like) with nav and a small health dot. Main area: compact one-row-per-account list (account, platform mark, game, channel, thin progress bar, ETA). Problem: the account's row itself turns red with an inline "Re-login" action; no banner.
03. **kanban-flow** — "Drop board". Kaneo-style. Left sidebar. Home shows drops as cards in three short columns: Mining, Up next, Claimed today. Problem: a red card pinned at top of Mining column.
04. **command-minimal** — "Command". No persistent nav; a centered single column, max ~640px. A command bar at top (⌘K hint) used for navigation. Status as a terse checklist ("Twitch ✓ mining", etc., drawn with SVG checks). Problem: the status line for that item turns red with a text link.
05. **today-timeline** — "Today timeline". Top tabs. Home is a vertical timeline: now (live progress) at top, then upcoming drops with ETAs, then earlier claims today. Accounts shown as small avatar chips on timeline items. Problem: an alert entry at the very top of the timeline.
06. **github-checks** — "Checks". GitHub-inspired. Top header like GitHub repo header with underline tabs. Home like a PR "checks" box: overall status summary ("2 successful, 1 failing") with expandable rows per account. Problem: shown as a failing check with "Details / Re-login".
07. **split-pane** — "Split pane". Mail-client style. Narrow icon rail for nav, a list column of accounts with status dots, and a detail pane showing the selected account (default selected: grubfan) with big progress. Problem: red dot on grub_alt in the list plus a count badge on the rail.
08. **game-cards** — "Game cards". Gaming feel but restrained. Top bar. Home is the chosen games as wide cards (use a dark solid/patterned block with the game name as art placeholder; no external images), each with campaign state and per-account progress. Problem: a small warning chip on the Rust card ("1 account needs login").
09. **kpi-strip** — "Pulse". Ramp-style calm dashboard. Left sidebar. Top: a status pill plus 3 KPIs (minutes mined today, drops claimed today, accounts healthy 2/3), then one inline SVG sparkline of minutes per hour today, then a short "Now mining" list. Problem: the "accounts healthy" KPI turns amber/red and is clickable.
10. **pocket-panel** — "Pocket". Mobile-first compact panel (like a menu-bar/tray widget), max ~420px centered on desktop, bottom tab bar with 4 icons. Big circular progress ring for the current drop, swipeable-looking account dots. Problem: a red pill above the ring.

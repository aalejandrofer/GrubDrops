# GrubDrops UI Design

Single source of truth for visual primitives. Reference pages: `/drops`
and `/` (Console). When adding a page/section, pick from these primitives;
do NOT invent ad-hoc styles inline.

## Core principle — FLAT

The app is **flat**: group content with **uppercase mono labels + a dashed
rule + spacing + a soft hover tint** — never with panel borders, filled
boxes, table chrome, or zebra striping. If you're reaching for a `border:
1px` box around a section, stop — use a dashed-rule section header instead.

- ❌ no panel/card borders or fills around sections (Currently Mining, Live
  Events, stat strip are all borderless)
- ❌ no `<table>` borders / header backgrounds / per-row border lines
- ❌ no zebra / alternating-row dual-tone
- ✅ dashed rule (`1px dashed var(--line)`) under section labels
- ✅ kind-colored 3px left accent on rows (`box-shadow: inset 3px 0 0 …`)
- ✅ soft `var(--surface-2)` hover on interactive rows

## Palette

CSS variables in `internal/web/static/css/app.css`. Always reference them;
never hard-code colors.

- `--bg`, `--bg-grad`; `--surface`, `--surface-2`; `--line`, `--line-soft`
- `--text`, `--muted`, `--dim`
- `--accent` (#d4631e), `--accent-2`; `--green` `--amber` `--blue` `--red`
- `--purple` (twitch), `--kick` (kick)

## Type

- **Display headings** (`h1` in `.ph`): Bricolage Grotesque, tight tracking.
- **Section labels / kicker / button labels**: JetBrains Mono, 10–11px,
  uppercase, letter-spacing ~0.14–0.18em, `--muted`.
- **Body / tabular data**: JetBrains Mono.
- Account names render the **display Label**, not the `@username`.

## Page skeleton

```html
<div class="shell">
  <div class="ph">
    <div><div class="kicker">// section</div><h1>Title</h1></div>
    <div class="actions"><a class="btn primary" href="…">Primary →</a></div>
  </div>
  <!-- sections … -->
</div>
```

## Section header (dashed rule)

```html
<header class="drops-pane-h"><h3>Recent claims</h3><span class="meta">12</span></header>
```
Mono uppercase title, muted count right, **dashed** bottom rule. On the
console, `.sec-head`/`.mining-col-head`/`.drawer-head` all use a dashed rule.

## Stat strip

Flat grid, no boxed cells: `.telemetry` is gap-separated, closed by a dashed
bottom rule. Each `.stat` is borderless with a 2px `--accent` top tick.
Never re-introduce the `gap:1px;background:var(--line)` boxed grid.

## Rows (lists, logs, claims, campaigns)

`.ev / .events` rows over `<table>`. Each row is a `<details>`:

```html
<details class="ev kind-{state|claim|progress|discovery|auth|error|info}">
  <summary>
    <span class="chev">›</span><span class="t">15:57</span>
    <span class="lvl" style="color:var(--…)">●</span>
    <span class="body"><em>kind</em> · message</span>
    <span class="ac">@account</span>
  </summary>
  <div class="ev-detail"><span class="kv"><span class="k">key</span><span class="eq">=</span><span class="v">val</span></span></div>
</details>
```
Kind sets a colored left accent. `.kv` chips are the canonical key/value
display in expanded rows. Keep all `<details>` expand behavior.

## Collection + link marks (/drops)

- **Collection**: `.collect-box` — a bordered box of green `✓` ticks (one per
  collector); orange `✗` (`.tk.cross`) for action-only campaigns that can't
  be auto-mined. Account identity doesn't matter — no `@login` text.
- **Connect chips**: `.conn-chip` — `✓ login` (green) when linked, `login →`
  (`.need`, accent) when an account that whitelists the game must connect.

## Forms (flat)

- Section labels: dashed-rule (`.sec` style), uppercase mono.
- Fields: `.inp` — subtle `var(--surface)` fill, **no box border**, bottom
  border that turns `--accent` on focus, 4px top radius. Caption above in
  uppercase mono `--muted`.
- Checkbox toggles (notify kinds): `.chk` with an accent-filled box when on.
- Don't use the old boxed `.form-card` look; flat fields on the page.

## Buttons

Two tiers — pick by placement, never invent a third:

- **Page-head actions** (`.ph .actions` only): `.btn`, `.btn.primary`
  (accent fill), `.btn.sm`, `.btn.ghost`. The filled box is reserved for
  the one primary page action (e.g. `+ Add account`).
- **Everything inside a section** (Save, Add, Send test, Change password,
  copy): **`.btn-linear`** — uppercase mono text + `→`, accent color, no
  border, no fill. Reads as a line action, not a box.

  ```html
  <button class="btn-linear" type="submit">Save →</button>
  ```

  Primary linear action stays `--accent`; a secondary one next to it gets
  `color:var(--muted)` (e.g. Send test beside Save). Right-align the
  primary (`margin-left:auto` in a `.row`).

Never inline `padding`/`font`/`border` on a button — adjust the class.
Never put a filled `.btn` box inside a flat section.

- **Destructive linear**: `.btn-linear.red` (Delete account). Lives in a
  `.danger-zone` strip — dashed top rule, red uppercase `.danger-label`
  left, the action right. No red boxes.
- **Alert CTAs** (console needs-auth banners): `.alert` is a flat row
  (3px accent left tick + soft tint, no box border); its `.alert-cta`
  renders linear via CSS override — keep the `.btn sm alert-cta` markup.

## Key-value rows (settings, status, read-outs)

STATUS-style dashed-leader rows are the canonical "table" for settings and
read-outs. Never `<table>`, never boxed inputs in settings sections.

- **Read-only** — `.kvlist` > `.kvrow` > `.k` / `.v` (Status panel, SSO
  info). Label muted left, value right, dashed rule between rows.
  An inline action on a value is a `.btn-linear` (e.g. callback `copy →`).
- **Editable** — `.kvedit` > `.row`:

  ```html
  <div class="kvedit">
    <div class="row">
      <span class="k">tick interval</span>
      <span class="d">how often it <b>thinks</b> — the watcher loop pulse</span>
      <span class="v"><input type="number" name="…"><span class="u">s</span></span>
    </div>
  </div>
  ```

  `.k` mono muted label · `.d` optional prose description (IBM Plex 11px)
  · `.v` borderless right-aligned input, underline only on hover/focus ·
  `.u` unit suffix.
- **Wide values** (URL, password, free text): add `.grow` to the `.v` —
  the input fills the row, left-aligned. Skip `.d`; fold hints into the
  placeholder (`https://…/avatar.png · optional`).
- **Checkbox group as a value**: `.v.cbs` with inline `label.cb` items
  (notify kinds row).
- Number-input spinner arrows are globally hidden in settings — values are
  typed. Don't re-enable.

## Subnav

`.subnav` + `.subnav-link` / `.subnav-link.on` (accent + light wash).

## Polling

`hx-trigger="every Ns"`, N ≥ 10. Sub-10s feels jumpy + breaks `<details>`
open state on swap.

## What NOT to do

- ❌ panel/section borders or filled boxes — flat + dashed rules only
- ❌ `<table>` for lists; table borders / header bg / row border-lines
- ❌ zebra / dual-tone alternating rows
- ❌ inline `<style>` blocks or inline font/color on chips/headings
- ❌ hard-coded colors — always `var(--…)`
- ❌ polling < 10s
- ❌ showing `@username` where the display Label is meaningful

## Adding a new page

1. Copy the `/drops` or `history.html` skeleton.
2. Flat sections: dashed-rule label + `.events` rows.
3. Test at ~1100px; rows must not overflow.
4. Update this file if a new primitive is needed.

# DropsMiner UI Design

Single source of truth for visual primitives. Reference page: `/history`.
When adding a new page or section, pick from these primitives; do NOT
invent ad-hoc styles inline.

## Palette

Defined as CSS variables in `internal/web/static/css/app.css`:

- `--bg`, `--bg-grad`: page background.
- `--surface`, `--surface-2`, `--surface-3`: card / panel / hover backgrounds.
- `--line`, `--line-soft`: dividers.
- `--text`, `--muted`, `--dim`: type hierarchy.
- `--accent` (#d4631e), `--accent-2`: primary actions, highlights.
- `--green`, `--amber`, `--blue`, `--red`: status colors.
- `--purple` (twitch), `--kick`: platform tints.

Always reference variables. Never hard-code colors in templates.

## Type

- **Display headings** (`h1` in `.ph`): Bricolage Grotesque, large, tight tracking.
- **Section labels / kicker / button labels**: JetBrains Mono, 10–11px, uppercase, letter-spacing ~0.14–0.18em, colored `--muted`.
- **Body text**: IBM Plex Sans, ~13px, `--text`.
- **Tabular data**: JetBrains Mono.

## Page skeleton

```html
<div class="shell">
  <div class="ph">
    <div>
      <div class="kicker">// section</div>
      <h1>Title</h1>
    </div>
    <div class="actions">
      <a class="btn primary" href="…">Primary →</a>
    </div>
  </div>

  <!-- Optional subnav -->
  <nav class="subnav">
    <a class="subnav-link on" href="…">Tab</a>
  </nav>

  <!-- Sections … -->
</div>
```

Reference: `internal/web/templates/history.html`.

## Section header

The `/history` row-list style is the canonical section header:

```html
<header class="drops-pane-h">
  <h3>Recent claims</h3>
  <span class="meta">{{len .Rows}}</span>
</header>
```

Monospace uppercase title, muted count on the right, dashed bottom border.

## Tables / rows

**Prefer `.ev / .events` rows over `<table>`** for any list with mixed
content (logs, claims, campaigns). Each row is a `<details>` with:

```html
<details class="ev kind-{state|claim|discovery|auth|error|info}">
  <summary>
    <span class="chev" aria-hidden="true">›</span>
    <span class="t">15:57:34</span>
    <span class="lvl" style="color:var(--{green|amber|blue|red|accent|muted})">●</span>
    <span class="body"><em>kind</em> · message text</span>
    <span class="ac">@account</span>
  </summary>
  <div class="ev-detail">
    <span class="kv"><span class="k">key</span><span class="eq">=</span><span class="v">value</span></span>
    …
  </div>
</details>
```

`.kv` chips inside `.ev-detail` are the canonical key/value display
inside expanded rows. Bordered, monospace, uppercase muted label.

When a real tabular layout is needed (small fixed columns), use
`.tbl` — but only inside a card or section, never as the primary
content. Default to `.events` rows.

## Cards

- `.form-card`: padded card for forms / configuration.
- `.drawer`: panel with `.drawer-head` (live events, live channels).

## Buttons

`.btn` (default), `.btn.primary`, `.btn.sm`, `.btn.ghost`. Never set
inline `padding` / `font` / `border` on a button — adjust the class.

## Modifiers

- Kind color on the left edge of `.ev`: `kind-claim` (green),
  `kind-progress` (amber), `kind-state` (blue), `kind-discovery`
  (dim), `kind-error` (red), `kind-auth` (accent), `kind-info` (line).
- Platform tint on the chevron / pip:
  `<span class="pip twitch|kick"></span>`.

## Subnav

```html
<nav class="subnav">
  <a class="subnav-link on" href="…">General</a>
  <a class="subnav-link"     href="…">Accounts</a>
</nav>
```

`.on` highlights the active tab with accent color + light wash.

## Polling

`hx-trigger="every Ns"` — pick N ≥ 10. Sub-10s polling makes the UI
feel jumpy and breaks `<details>` open state on swap. Live channels +
live events poll every 10s; mining cards every 10s.

## What NOT to do

- ❌ Inline `<style>` blocks per template — push to `app.css`.
- ❌ Inline `style="font-family:'JetBrains Mono'…"` per chip — use `.kv`.
- ❌ Inline `style="font-family:'Bricolage Grotesque'…"` per heading — use `<h1>` inside `.ph`.
- ❌ `<table>` for list views — use `.events` rows.
- ❌ Hard-coded colors — always `var(--…)`.
- ❌ Polling intervals < 10s.

## Adding a new page

1. Copy `history.html` skeleton.
2. Replace title + sections.
3. Use `.events` rows by default.
4. Test in narrow viewport (~1100px); rows must not overflow.
5. Update this file if a new primitive is needed.

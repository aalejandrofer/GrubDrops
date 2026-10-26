# GrubDrops UI Design

Reference for the visual primitives of the SvelteKit UI in `web/`. Tokens live
in `web/src/app.css`; each component and page keeps its own CSS in a `<style>`
block. Reference tokens, never hard-coded colors.

## Themes

Dark is the default (`:root`). Light is `:root[data-theme='light']`. The
`theme` setting (Settings → General) sets `data-theme` on `<html>`.

| Token | Dark | Light |
|---|---|---|
| `--bg` | `#0d1117` | `#ffffff` |
| `--surface` / `--surface-2` / `--surface-3` | `#161b22` / `#1c2129` / `#262c36` | `#f6f8fa` / `#eaeef2` / `#d0d7de` |
| `--border` / `--border-soft` | `#30363d` / `#21262d` | `#d0d7de` / `#d8dee4` |
| `--text` / `--muted` / `--dim` | `#e6edf3` / `#8b949e` / `#6e7681` | `#1f2328` / `#59636e` / `#6e7781` |
| `--accent` / `--accent-strong` | `#a371f7` / `#8957e5` | `#8250df` / `#6639ba` |

Status colors: `--green`, `--amber`, `--red`, `--blue`, each with a `-soft`
tint for backgrounds. Platform colors: `--twitch` (`#9146ff`) and `--kick`
(`#53fc18`), the same in both themes. `--art-bg` fills box art that has not
loaded. `--t2` and `--t3` are secondary text steps.

## Type and shape

- Body: `--font`, `Inter, system-ui, -apple-system, "Segoe UI", sans-serif`,
  14px / 1.5.
- Numbers, ids, commands and log lines: `--mono`, `"JetBrains Mono",
  ui-monospace, SFMono-Regular, Menlo, monospace`.
- No web fonts load; both stacks resolve to installed fonts.
- Corner radius `--radius` (8px); game cards use 10px.
- Icons come from `Icon.svelte`; platform badges from `PlatformMark.svelte`.

## Layout

- Top bar 52px high, content container max width 1160px.
- Breakpoints: 960px (Home collapses its columns), 720px (top bar, Home,
  Games, Discover, Rewards and Settings go single column), 420px (top bar and Settings
  tighten).
- Pages render a `PageHead` (title, optional timestamp and status, actions in
  its snippet) above their sections.

## Pages

Routes under `web/src/routes/`:

- `(app)`: Home, Games (`games/`), Discover (`discover/`), Rewards
  (`rewards/`), Settings
  (`settings/`) with sections General, Accounts, Notifications, Updates and
  Health, each its own component. `welcome/` is the first-run wizard (add one or
  more accounts, pick games; Start mining goes Home), full screen without the top bar.
  Home sends a first run with no accounts there.
- `(auth)`: Login (`login/`) and first-run Setup (`setup/`): a centered card
  with the host below it, styled by `auth.css`. Password first, then SSO
  under an "or" rule when OIDC is on.

## Components

In `web/src/lib/components/`:

| Component | Use |
|---|---|
| `TopBar` | Brand, page links, version and update indicator. |
| `VersionPopover` | Current version and release notes, opened from the top bar. |
| `PageHead` | Page title row. |
| `GameCard` | One game on Home: platform, progress, state, last scan. |
| `UpNext` | An account's queued drops. |
| `BoxArt` | Game art with a placeholder, sizes `lg` and `sm`. |
| `PlatformMark` | Twitch or Kick mark, sizes `md` and `sm`. |
| `Icon` | Inline SVG icons by name. |
| `CopyCommand` | A command with a copy button. |
| `AccountFlow` | Add or re-login one account: Twitch Android paste, Twitch TV code, Kick cookies.txt. Used by Settings > Accounts and the first-run wizard. |

## Rules

- Every user-facing string goes through `t()`/`tn()`. Server strings (game
  names, drop titles, log messages, release notes) render as text; `{@html}`
  is never used.
- Copy states facts ("Last scan 41 s ago"). No filler lines.
- Status is a token color plus a text label, never color alone.
- Svelte 5 runes only (`$state`, `$derived`, `$effect`, `$props`).
- A new primitive goes into `app.css` or a component, then into this file.

# SSO UI — login redesign + settings card design

Date: 2026-06-10
Branch: `feat/sso` (extends PR #9)
Status: approved (design)

## Goal

Two UI changes on top of the now-working OIDC SSO:

1. **Login page** — make SSO the primary call to action and keep the admin
   password visible-but-secondary below it. The SSO button must NOT name the
   provider; it reads "Continue with SSO". When OIDC is disabled the page falls
   back to the password-only form it shows today.
2. **Settings** — add a read-only "Single sign-on (SSO)" card to the General
   settings stack showing the current OIDC status (config is env-only, so this
   is informational, not editable).

No new env vars, no DB changes. All on `feat/sso`.

## Background (current state)

- `internal/web/templates/login.html` renders one `.auth-card`: a password
  form, plus (when `OIDCEnabled`) an `.sso-row` with `.sso-divider` and a
  button labelled `Sign in with {{.OIDCProviderName}}`. The divider/row classes
  are referenced but not styled in `app.css`.
- `templateData` (internal/api/render.go) carries `OIDCEnabled bool` and
  `OIDCProviderName string`. `authDeps.loginGet` populates them.
- Settings is server-rendered (`settings.html`) as a `.settings-stack` of
  `.settings-card` sections, fed by `settingsPageData` from
  `settingsDeps.get` (internal/api/handlers_settings.go). A subnav links
  General / Accounts.
- `app.css` is the single stylesheet, dark theme with CSS-var tokens
  (`--accent` #d4631e, `--green`, `--surface`, `--line`, `--muted`, etc.).
- `oidc.Provider` exposes `Enabled() bool` and `Name() string`; it stores the
  oauth2 config (with RedirectURL) and `allowedEmails`/`allowedGroups`, but does
  not yet expose the issuer or those lists.

## Design

### 1. Login page

`login.html`, rendered states:

- **OIDC enabled:**
  1. Full-width primary button `⮕ Continue with SSO` → `GET /auth/oidc/login`
     (static label — no provider name interpolation).
  2. An `or` divider (`.sso-divider`).
  3. The password form below, visually de-emphasized: same fields, a secondary
     (non-primary) submit button. Still posts to `/login` with CSRF.
- **OIDC disabled:** password form only, primary styling (today's layout). No
  button, no divider.

The flash message and CSRF token render in both states as today.

`OIDCProviderName` is no longer used by the login template (button is static),
but the field stays on `templateData` because the settings card uses the
provider name. `loginGet` is otherwise unchanged.

### 2. CSS (`app.css`)

Add, using existing tokens:

- `.btn-sso` — full-width primary-styled button (accent background, larger hit
  area) for the SSO CTA.
- `.sso-divider` — horizontal "or" rule: a centered `or` with lines either side
  (flexbox + `--line` borders, `--muted` text).
- De-emphasis for the secondary password block when SSO is present (e.g. a
  `.auth-secondary` wrapper: muted label, standard `.btn` not `.btn primary`).
- Read-only settings status styling (see below): `.sso-status` rows
  (label/value pairs), a status pill reusing `--green` (enabled) / `--muted`
  (disabled), and a copyable `.copy-field` (monospace value + small copy
  button). Reuse existing `.kicker`/`.meta`/`.hint` where they fit.

No framework; match the existing hand-rolled CSS conventions.

### 3. SSO settings card

`oidc.Provider` gains read-only getters (single source of truth, config flows
through the provider):

- `Issuer() string` — store the configured issuer on the struct in `New`.
- `RedirectURL() string` — from the oauth2 config.
- `AllowedEmails() []string`, `AllowedGroups() []string` — return the stored
  (already trimmed) lists.

(`Name()` and `Enabled()` already exist.)

`settingsDeps` gains an `oidc *oidc.Provider` field, wired from `Deps.OIDC` in
`server.go`. `settingsPageData` gains an `OIDC` sub-struct:

```
type settingsOIDC struct {
    Enabled      bool
    ProviderName string
    Issuer       string
    CallbackURL  string
    AllowedEmails []string
    AllowedGroups []string
}
```

`settingsDeps.get` populates it from the provider (zero-value/Enabled=false
when `d.oidc == nil || !d.oidc.Enabled()`).

`settings.html` renders a new `.settings-card` "Single sign-on (SSO)" in the
General stack:

- **Enabled:** green status pill; rows for Provider, Issuer, Allowlist
  (join emails+groups, or "Any IdP user — access gated by your identity
  provider" when both empty), and a copyable Callback URL. Footnote:
  "Configured via `GRUB_OIDC_*` environment variables (read-only here)."
- **Disabled:** muted status pill; "Not configured — set `GRUB_OIDC_*` to
  enable SSO." with a short hint pointing at the README section.

The card is purely informational; no form, no POST.

## Files

Modify:
- `internal/web/templates/login.html` — two-state SSO-primary layout.
- `internal/web/static/css/app.css` — `.btn-sso`, `.sso-divider`,
  `.auth-secondary`, `.sso-status`/pill/`.copy-field`.
- `internal/auth/oidc/oidc.go` — store issuer; add `Issuer`, `RedirectURL`,
  `AllowedEmails`, `AllowedGroups` getters.
- `internal/api/handlers_settings.go` — `settingsDeps.oidc` field,
  `settingsOIDC` struct on `settingsPageData`, populate in `get`.
- `internal/api/server.go` — pass `d.OIDC` into the `settingsDeps` literal.
- `internal/web/templates/settings.html` — the SSO card.

Tests:
- `internal/auth/oidc/oidc_test.go` — getter assertions (enabled provider
  returns issuer/redirect/lists; trimming preserved).
- `internal/api/handlers_auth_test.go` (or existing login test) — SSO button
  present when `OIDCEnabled`, absent when not; label is "Continue with SSO".
- `internal/api/handlers_settings_test.go` — settings renders enabled status
  (provider/issuer/callback) and the disabled state.

A tiny copy-to-clipboard for the callback URL is inline vanilla JS in the
template (the codebase already uses small inline JS, e.g. the priority picker);
no new asset.

## Error handling

None new — read-only rendering. Disabled/nil provider degrades to the
password-only login and the "not configured" settings card. Existing OIDC flow
and handlers are untouched.

## Out of scope

- Editing OIDC config from the UI (stays env-only).
- Multiple/again provider buttons (single generic SSO button).
- Any change to the auth flow, handlers, or session model.

# SSO / OIDC support — design

Date: 2026-06-10
Branch: `feat/sso`
Status: approved (design)

## Goal

Add generic OIDC single-sign-on to grubdrops while keeping the existing
admin password as a break-glass login. Must work with any compliant IdP
(authentik, Auth0, Keycloak, Google, Okta, Azure AD, …) — this is a public
project, so it cannot be authentik-specific. SAML is explicitly out of scope
for v1 (can be added later behind the same provider abstraction if anyone
needs an OIDC-incapable IdP).

## Core insight

Authentication today is a single session boolean, `admin_authed`, managed by
`scs` with a kv-table-backed store (`internal/api/session.go`). The
`RequireAdmin` middleware (`internal/api/middleware.go`) gates the entire
authed area on that boolean.

OIDC is therefore just **another way to flip that boolean true**. There is no
multi-user model, no RBAC, and no per-user records. Both the password flow and
the OIDC flow end in the same place: `RenewToken` + `admin_authed = true`.

Consequence: **no database migration is required.**

## Authorization model

Single-tenant. The miner has exactly one privilege level (root admin).

- Anyone who can authenticate against the **configured issuer** is granted the
  admin session.
- Optional tightening, both env-driven:
  - `GRUB_OIDC_ALLOWED_EMAILS` — comma-separated allowlist matched against the
    verified `email` claim (case-insensitive, trimmed).
  - `GRUB_OIDC_ALLOWED_GROUPS` — comma-separated; the token must carry at
    least one matching value in its `groups` claim.
- If both are empty, any successful authentication against the issuer is
  accepted. Membership is controlled in the IdP itself.

The password flow is unchanged and maps to the single root admin row
(`admin.id = 1`). It remains as a fallback path if the IdP is unreachable.

## Configuration (environment variables)

Consistent with the existing 12-factor config in `internal/config/config.go`
(master key, Discord webhook, etc. are all env). Secrets stay out of the DB.

```
GRUB_OIDC_ISSUER          e.g. https://authentik.example.com/application/o/grubdrops/
GRUB_OIDC_CLIENT_ID       OAuth client id
GRUB_OIDC_CLIENT_SECRET   OAuth client secret
GRUB_OIDC_REDIRECT_URL    e.g. https://grub.example.com/auth/oidc/callback
GRUB_OIDC_PROVIDER_NAME   button label, default "SSO"
GRUB_OIDC_ALLOWED_EMAILS  optional, comma-separated
GRUB_OIDC_ALLOWED_GROUPS  optional, comma-separated
```

OIDC is **enabled only when** `GRUB_OIDC_ISSUER`, `GRUB_OIDC_CLIENT_ID`,
`GRUB_OIDC_CLIENT_SECRET`, and `GRUB_OIDC_REDIRECT_URL` are all non-empty.
When disabled, the login page shows the password form only and the OIDC routes
return 404 / redirect to `/login`.

Discovery is automatic via the issuer's `.well-known/openid-configuration`,
which is what makes the implementation generic across IdPs.

## Library

- `github.com/coreos/go-oidc/v3/oidc` — discovery, JWKS, ID-token verification.
- `golang.org/x/oauth2` — authorization-code exchange.

No hand-rolled crypto or token parsing.

## Flow

1. **`GET /login`** — renders password form plus, when OIDC is enabled, a
   "Sign in with {ProviderName}" button linking to `/auth/oidc/login`.
2. **`GET /auth/oidc/login`** — generate `state`, `nonce`, and PKCE verifier;
   persist `{nonce, pkce, expiry}` server-side in the `kv` table under key
   `oidc:<state>`; set a short-lived (5 min) transient cookie
   `grub_oidc_state=<state>` (`SameSite=Lax`, `HttpOnly`, `Secure` per config);
   redirect to the IdP authorize endpoint with scopes `openid profile email`
   (plus `groups` when group allowlist is set).
3. **`GET /auth/oidc/callback`**:
   - Read `state` from the query and from the `grub_oidc_state` cookie; they
     must match (binds the callback to this browser).
   - Look up `oidc:<state>` in the kv store and delete it immediately
     (single-use). Missing/expired → reject.
   - Exchange the `code` for tokens (with PKCE verifier).
   - Verify the ID token via go-oidc (signature, issuer, audience, expiry).
   - Verify the `nonce` claim matches the session value.
   - Apply the email/group allowlist check.
   - On success: `RenewToken` (session fixation defence) + `admin_authed = true`
     + optionally stash `auth_identity` (email/sub) for display; redirect `/`.
   - On any failure: redirect to `/login` with a flash message; never leak raw
     errors.

Both OIDC routes run through the session and CSRF middleware like the existing
public routes. They are GET requests, so nosurf passes them through; the OIDC
`state` parameter provides the callback's CSRF guarantee.

## Components / files

New:

- `internal/auth/oidc/` — package owning the OIDC integration:
  - `Config` struct + constructor from `config.Config`.
  - `Provider` wrapping `*oidc.Provider`, `*oidc.IDTokenVerifier`, and
    `oauth2.Config`.
  - `AuthCodeURL(state, nonce, pkce)` helper.
  - `Exchange + Verify` returning verified claims.
  - `Authorize(claims) error` — the email/group allowlist check.
  - state/nonce/PKCE generation helpers.
  - A kv-backed handshake store (`Put(state, nonce, pkce)` /
    `Take(state) -> nonce, pkce`) keyed `oidc:<state>` with a 5-minute expiry,
    reusing the existing `kv` table (distinct prefix from `session:`).
  - `Enabled()` predicate.
- `internal/api/handlers_oidc.go` — `oidcDeps` with `loginRedirect` and
  `callback` handlers; depends on the oidc.Provider and the session manager.

Changed:

- `internal/config/config.go` — add OIDC fields and parsing; no hard
  requirement (absent config simply disables the feature).
- `internal/api/handlers_auth.go` and the `templateData` struct — surface
  `OIDCEnabled bool` and `OIDCProviderName string` to the login template.
- `internal/web/templates/login.html` — conditional SSO button.
- `internal/api/server.go` — build `oidcDeps`, register `GET /auth/oidc/login`
  and `GET /auth/oidc/callback` as public (session + CSRF) routes; thread the
  provider through `Deps`.
- `cmd/miner/main.go` — construct the provider from config at startup
  (network call to the discovery endpoint; failure logs a warning and leaves
  OIDC disabled rather than crashing the miner) and pass it into `Deps`.

No DB migration. No new tables or queries.

## Error handling

- Provider construction failure at startup → log warning, OIDC stays disabled,
  password login still works. The miner must never fail to boot because the
  IdP is down.
- Callback failures (bad state, exchange error, verification failure, nonce
  mismatch, allowlist rejection) → redirect to `/login` with a generic flash;
  detailed cause logged server-side only.
- Routes are no-ops (redirect to `/login`) when OIDC is disabled.

## Security considerations

- PKCE on the authorization-code flow.
- `state` + `nonce`, both single-use. The main scs session cookie is
  `SameSite=Strict` and is therefore NOT sent on the cross-site redirect back
  from the IdP — so the handshake (`nonce`, PKCE verifier) is stored
  server-side in `kv` keyed by `state`, with a short-lived `SameSite=Lax`
  transient cookie carrying only the `state` value to bind the callback to the
  originating browser. The kv record is deleted on first use and expires after
  5 minutes.
- Full ID-token verification through go-oidc (sig/iss/aud/exp).
- `RenewToken` on successful login to prevent session fixation.
- Honour the existing `SecureCookies` config for both the session cookie and
  the transient `grub_oidc_state` cookie.
- No OAuth tokens persisted; only the transient handshake record (5 min) and,
  after success, the `admin_authed` bool (plus optional display identity) in
  the session.

## Testing

- **Unit**
  - Config parsing and the enabled/disabled predicate.
  - Allowlist logic: empty (allow all), email match/miss, group match/miss,
    case/whitespace handling.
  - state/nonce/PKCE generation (non-empty, sufficient entropy, distinct).
- **Integration**
  - In-process fake OIDC server via `httptest`, serving
    `.well-known/openid-configuration`, a JWKS endpoint, and a token endpoint
    that mints a signed ID token. Drive the full
    `loginRedirect → callback` path against the real go-oidc verifier and
    assert `admin_authed` is set on success and not set on each failure mode.

## Out of scope (v1)

- SAML.
- Multi-user accounts / RBAC.
- A settings-UI page for OIDC config (env only).
- Auto-provisioning user records (none exist).

# Kick session longevity — deferred, needs redesign

**Date:** 2026-08-04
**Status:** deferred, pre-design. Do NOT implement from the old plan.
**Supersedes:** item 3 and item 4 of `2026-08-04-operator-chores-design.md`

An implementation of Kick cookie capture was built, reviewed, and **reverted**
before merge on branch `feat/operator-chores`. The code is recoverable from git
history (commits `56cfc45`, `a1320d0`, `5c81c47`, reverted together). This file
records why it was reverted and what any future attempt must decide first, so
the same work is not redone from the same wrong premises.

## Why it was reverted

The implementation was correct against its plan. The plan was wrong.

### 1. The change-detection baseline was the caller's session, so it wrote forever

`captureCookies` merged each response's `Set-Cookie` into the session **the
caller passed in**, and compared against that. But `internal/watcher`'s session
is assigned once at construction (`watcher.go:266,270`) and thereafter only
read — ten call sites, never reassigned. The `freshest` map was written but
never consulted as the baseline.

So after a single rotation the watcher kept sending the old cookie, the server
kept reissuing the new one, and **every subsequent response looked like a
change**: an age-encrypt plus a `sessions` upsert per response, indefinitely,
against a database with `SetMaxOpenConns(1)` (`internal/store/db.go:24`). That
serializes against the dashboard poll's progress writes, the watcher's own
writes, and the authcheck sweep.

Worse, the write ran **inside the response path before the body was read**
(`transport.go`, between `defer resp.Body.Close()` and `io.ReadAll`), holding
an HTTP/2 stream open on the shared cached `ClientConn` while blocking on the
DB. Under contention that stalls the Kick transport and can surface as a Kick
API error.

### 2. Capture was not gated on HTTP status, so a 401 could clobber a fresh login

Concrete sequence: the operator re-pastes cookies; `persistKickSession` writes
the fresh session and then reloads the scheduler; an in-flight request from the
outgoing watcher generation still carries the dead cookies, gets a 401, and
Laravel attaches a fresh **anonymous** session cookie. That guest cookie was
merged into the stale blob and persisted, overwriting the login the operator
had just pasted. The re-login silently did not take.

### 3. Lost update between two concurrent writers

The hourly authcheck sweep and the watcher both did read-modify-write on the
same account's session using their own snapshots. `Backend.mu` guarded only the
`freshest` map, not the store round trip. The sweep could therefore persist a
pre-rotation `session_token` alongside a post-rotation `kick_session` — the
component whose job is detecting dead sessions could create one.

### 4. It could not deliver its stated goal anyway

Three things must be true for "a Kick login outlives its original
`cookies.txt` paste." None was:

- **Kick must actually rotate these cookies.** Still unverified. Nobody has
  observed a rotation; the whole feature rests on an assumption.
- **The running process must use the rotated cookie.** It did not.
  `RefreshSession` is unreachable for Kick in production (its only caller,
  `cmd/miner/main.go:407`, requires a non-empty `RefreshToken`, which a Kick
  session never has), and the watcher holds an immutable session. The fresh
  cookie reached disk and never the wire.
- **The session must survive the boot-time gate.** It did not.
  `handlers_login_kick.go:175` pins `ExpiresAt` at login + 7 days, and a
  rotation deliberately preserves `ExpiresAt` rather than advancing it. So
  `cmd/miner/main.go:401` idles the account after 7 days regardless of how
  fresh its cookies are.

Note the interaction that makes this worse: in that 7-day-gate scenario the
auth-health notification stays **silent**, because authcheck verifies the
cookies, finds them working, and records `OK` while the scheduler idles the
account.

### 5. It excluded the cookie most likely to go stale

The merge accepted only `session_token`, `XSRF-TOKEN`, and `kick_session`. But
the login handler explicitly stores and sends `cf_clearance`
(`handlers_login_kick.go:151-153`), and Cloudflare reissues that on its own
schedule. Leaving it stale is a plausible mechanism for the original
"account silently stops mining" symptom — arguably a likelier one than session
cookie expiry.

## What a future attempt must decide before writing code

1. **Does Kick actually rotate any of these cookies?** Settle this first, with
   a live session and a log of observed `Set-Cookie` headers. If it does not,
   most of this work has no purpose and the auth-health alert (shipped) is the
   whole answer. This is a measurement task, not a coding task.
2. **Where does the rotated cookie live so the running watcher uses it?** Disk
   alone is not enough. This needs a real answer about session ownership: the
   watcher's session is immutable by design, so either it gains a refresh path
   or the backend holds the authoritative copy.
3. **Should `ExpiresAt` advance on rotation?** Without this, nothing extends a
   Kick login past 7 days. With it, the boot-time expiry gate stops meaning
   what it currently means for Kick — decide deliberately.
4. **Is `cf_clearance` in scope?** It is IP-bound, which has its own
   implications for whether persisting it is safe.
5. **Where does the persist happen?** Not on the response path, and not
   unserialized against the authcheck sweep.

## What was worth keeping

Two pieces of that implementation were genuinely good and should be reused:

- **`patchKickSession`** — patches only the cookie fields inside the stored
  JSON blob, preserving keys the `kick` package does not model. This exists
  because `kickSession` models only `cookies`/`xsrf_token`/`user_agent`, while
  the login handler writes `channel`/`channels` too, so the obvious
  decode-then-re-encode round trip silently drops the account's channel list
  and zeroes `ExpiresAt`. It also copies the cookie map rather than mutating
  the caller's live one.
- **Domain/Path inheritance for a newly added cookie.** A cookie with an empty
  `Domain` fails CDP's `Network.setCookie`, and the sidecar's `InstallCookies`
  aborts the entire install on its first error — so one domainless cookie would
  silently break the browser watch tab's auth while leaving the HTTP path fine.

## The other deferred piece

Round-tripping cookies back out of the Chrome watch sidecar was deferred for a
separate reason: it needs a **protobuf change**, which the original plan wrongly
claimed it did not. `KickSession` carries a `Cookies` field, but it appears only
in request messages — `StartWatchResponse` is `{watch_handle}`,
`HeartbeatResponse` is `{alive}`, and `StopWatchResponse` is empty. Nothing
carries a session back. That work needs a proto field, `buf generate`, and
signature changes through `browser.Client`.

If it is ever done, `Heartbeat` is likely the better hook than `StopWatch`: a
watch can run for hours, and the process may restart before `StopWatch` ever
fires.

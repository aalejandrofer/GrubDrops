# Helper Removal + cookies.txt Login + Docker-First Deploy Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Remove the helper CLI distribution channel entirely, replace Kick cookie ingestion with a cookies.txt (browser-extension export) paste/upload form, publish both docker images to ghcr.io on tag, and rewrite the README deployment-first.

**Architecture:** Pure-removal of `cmd/grubdrops-helper*` + `internal/helper` + two HTTP routes; one new Netscape-format parser in `internal/api` feeding the existing `persistKickSession`; one new GitHub Actions workflow; README/template copy changes. Sidecar/browser stack (PR #12, credit-earning Kick watch) is NOT touched.

**Tech Stack:** Go 1.26, chi, Go html/template (HTMX UI), GitHub Actions, docker buildx, ghcr.io.

**Spec:** `docs/superpowers/specs/2026-06-12-helper-removal-cookies-txt-deploy-design.md`

---

### Task 1: Remove helper ingest endpoint + download endpoint + HelperDir

**Files:**
- Modify: `internal/api/server.go` (route at :179, `HelperDir` field at :88-90, `dlH` wiring at :300-301)
- Modify: `internal/api/handlers_login_kick.go` (delete `helperIngest`, :93-125)
- Delete: `internal/api/handlers_download.go`
- Delete: `internal/api/handlers_helper_ingest_test.go` (its DB/session test helpers move to `handlers_login_kick_test.go` in Task 3)
- Modify: `cmd/miner/main.go:443` (`HelperDir` dep)

- [ ] **Step 1: Move reusable test helpers, delete the ingest test file**

Create `internal/api/handlers_login_kick_test.go` containing the `ageKey` const and `newKickIngestDeps` helper copied verbatim from `handlers_helper_ingest_test.go`, renamed `newKickLoginDeps` (same body, same imports: context, path/filepath, testing, time, store, gen). Drop `postKickIngest` and both `TestHelperIngest_*` tests. Then `git rm internal/api/handlers_helper_ingest_test.go`.

```go
package api

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/aalejandrofer/grubdrops/internal/store"
	"github.com/aalejandrofer/grubdrops/internal/store/gen"
)

// ageKey is a throwaway age identity for encrypting stored sessions in tests.
const ageKey = "AGE-SECRET-KEY-1DZCAXYWJM6M42NSX5GR4QWZZ2JXEYKJ9ZKWYFYSNU997775JJ6XSY85FK9"

// newKickLoginDeps spins up a migrated sqlite store + a kick account and
// returns deps wired for the Kick login handlers.
func newKickLoginDeps(t *testing.T, accID string) (*loginKickDeps, *store.SessionStore) {
	t.Helper()
	db, err := store.Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	q := gen.New(db)
	if accID != "" {
		now := time.Now().Unix()
		if _, err := q.CreateAccount(context.Background(), gen.CreateAccountParams{
			ID: accID, Platform: "kick", DisplayName: "TTik3r",
			Status: "idle", FingerprintJson: "{}", Enabled: 1,
			CreatedAt: now, UpdatedAt: now,
		}); err != nil {
			t.Fatalf("seed account: %v", err)
		}
	}
	cr, err := store.NewCryptor(ageKey)
	if err != nil {
		t.Fatalf("cryptor: %v", err)
	}
	ss := store.NewSessionStore(db, q, cr)
	return &loginKickDeps{q: q, sessions: ss}, ss
}
```

- [ ] **Step 2: Delete handler code**

- In `internal/api/handlers_login_kick.go`: delete the `helperIngest` method and its doc comment (lines 93-125).
- `git rm internal/api/handlers_download.go`.
- In `internal/api/server.go`:
  - delete the route + its 4-line comment block (`r.Post("/helper/accounts/{id}/kick", loginKick.helperIngest)` and the comment above it, around :175-179)
  - delete the `HelperDir string` field and its comment from the `Deps` struct (:88-90)
  - delete the two `dlH` lines (`dlH := &downloadDeps{dir: d.HelperDir}` / `authed.Get("/download/helper", dlH.helper)`, :300-301)
- In `cmd/miner/main.go`: delete `HelperDir: os.Getenv("GRUB_HELPER_DIR"),` (:443).

- [ ] **Step 3: Verify build + tests**

Run: `go build ./... && go vet ./... && go test ./internal/api/`
Expected: PASS (the moved helper compiles; no references to `helperIngest`/`downloadDeps` remain).

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "feat(api)!: remove helper ingest + download endpoints

POST /helper/accounts/{id}/kick and GET /download/helper are gone.
Cookie ingestion moves to the authed cookies.txt form (next commits);
remote users authenticate via SSO instead of the account-id-as-secret
ingest URL."
```

---

### Task 2: Delete helper binaries, library, build script, Dockerfile stage

**Files:**
- Delete: `cmd/grubdrops-helper/`, `cmd/grubdrops-helper-gui/`, `internal/helper/`, `scripts/build-helper-gui.sh`
- Modify: `deploy/Dockerfile.miner` (drop lines 20-32 cross-compile stage + `/helpers` COPY at line 36)
- Modify: `go.mod`/`go.sum` via `go mod tidy`
- Modify: `.env.example` (drop `GRUB_HELPER_DIR` if present)

- [ ] **Step 1: Delete directories + script**

```bash
git rm -r cmd/grubdrops-helper cmd/grubdrops-helper-gui internal/helper scripts/build-helper-gui.sh
```

- [ ] **Step 2: Trim Dockerfile.miner**

Remove the helper cross-compile RUN block (lines 20-32, comment included) and the `COPY --from=build /helpers /helpers` line. Final stage keeps only the miner binary + CA certs.

- [ ] **Step 3: Drop env var doc**

`grep -n GRUB_HELPER_DIR .env.example deploy/docker-compose.yml` — remove any hit (README handled in Task 6).

- [ ] **Step 4: go mod tidy**

Run: `go mod tidy && git diff --stat go.mod`
Expected: `github.com/browserutils/kooky` (and its transitive deps) removed from go.mod.

- [ ] **Step 5: Verify**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: PASS. Then `grep -rn "kooky\|GRUB_HELPER_DIR\|grubdrops-helper" --include="*.go" --include="*.html" --include="Dockerfile*" --include="*.sh" cmd/ internal/ deploy/ scripts/` → no hits.

- [ ] **Step 6: Commit**

```bash
git add -A
git commit -m "chore!: delete helper CLI, GUI, library and image cross-compile"
```

---

### Task 3: cookies.txt parser (TDD)

**Files:**
- Create: `internal/api/cookies_netscape.go`
- Create: `internal/api/cookies_netscape_test.go`

- [ ] **Step 1: Write failing tests**

```go
package api

import (
	"strings"
	"testing"
)

const cookiesTxtOK = "# Netscape HTTP Cookie File\n" +
	"# This is a generated file! Do not edit.\n" +
	"\n" +
	".kick.com\tTRUE\t/\tTRUE\t1781000000\tkick_session\tsess-val\n" +
	"#HttpOnly_.kick.com\tTRUE\t/\tTRUE\t1781000000\tsession_token\ttok-val%7Cabc\n" +
	".kick.com\tTRUE\t/\tTRUE\t1781000000\tXSRF-TOKEN\txsrf-val\n" +
	".kick.com\tTRUE\t/\tTRUE\t1781000000\tcf_clearance\tcf-val\n" +
	".example.com\tTRUE\t/\tTRUE\t1781000000\tkick_session\tWRONG-DOMAIN\n"

func TestKickCookiesFromNetscape_HappyPath(t *testing.T) {
	f, err := kickCookiesFromNetscape(cookiesTxtOK)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.KickSession != "sess-val" {
		t.Errorf("KickSession = %q, want sess-val (kick.com row, not example.com)", f.KickSession)
	}
	if f.SessionToken != "tok-val%7Cabc" {
		t.Errorf("SessionToken = %q (HttpOnly_ prefix must be tolerated)", f.SessionToken)
	}
	if f.XSRF != "xsrf-val" || f.CFClearance != "cf-val" {
		t.Errorf("XSRF=%q CFClearance=%q", f.XSRF, f.CFClearance)
	}
}

func TestKickCookiesFromNetscape_MissingRequired(t *testing.T) {
	// session_token absent → error must name it.
	raw := ".kick.com\tTRUE\t/\tTRUE\t1781000000\tkick_session\tsess\n"
	_, err := kickCookiesFromNetscape(raw)
	if err == nil || !strings.Contains(err.Error(), "session_token") {
		t.Fatalf("want error naming session_token, got %v", err)
	}
}

func TestKickCookiesFromNetscape_NoKickRows(t *testing.T) {
	raw := "# Netscape HTTP Cookie File\n.example.com\tTRUE\t/\tTRUE\t1\tfoo\tbar\n"
	_, err := kickCookiesFromNetscape(raw)
	if err == nil || !strings.Contains(err.Error(), "kick.com") {
		t.Fatalf("want 'no kick.com cookies' error, got %v", err)
	}
}

func TestKickCookiesFromNetscape_GarbageAndCRLF(t *testing.T) {
	raw := "not a cookie line\r\n\r\n.kick.com\tTRUE\t/\tTRUE\t1\tkick_session\ts\r\n" +
		".kick.com\tTRUE\t/\tTRUE\t1\tsession_token\tt\r\n"
	f, err := kickCookiesFromNetscape(raw)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if f.KickSession != "s" || f.SessionToken != "t" {
		t.Errorf("CRLF parse: got %+v", f)
	}
}
```

- [ ] **Step 2: Run tests, verify failure**

Run: `go test ./internal/api/ -run TestKickCookiesFromNetscape -v`
Expected: FAIL — `undefined: kickCookiesFromNetscape`.

- [ ] **Step 3: Implement parser**

```go
package api

import (
	"fmt"
	"strings"
)

// kickCookiesFromNetscape parses a Netscape cookies.txt export (the format
// browser extensions like "Get cookies.txt LOCALLY" produce) and extracts the
// kick.com cookies the miner needs. Lines are 7 tab-separated fields:
// domain, includeSubdomains, path, secure, expiry, name, value. '#' lines are
// comments, except the '#HttpOnly_' domain prefix some exporters emit.
func kickCookiesFromNetscape(raw string) (kickCookieForm, error) {
	var f kickCookieForm
	sawKick := false
	for _, line := range strings.Split(raw, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || (strings.HasPrefix(line, "#") && !strings.HasPrefix(line, "#HttpOnly_")) {
			continue
		}
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}
		domain := strings.TrimPrefix(fields[0], "#HttpOnly_")
		if !isKickDomain(domain) {
			continue
		}
		sawKick = true
		switch fields[5] {
		case "kick_session":
			f.KickSession = fields[6]
		case "XSRF-TOKEN":
			f.XSRF = fields[6]
		case "cf_clearance":
			f.CFClearance = fields[6]
		case "session_token":
			f.SessionToken = fields[6]
		}
	}
	if !sawKick {
		return f, fmt.Errorf("no kick.com cookies found — export cookies.txt while on kick.com")
	}
	var missing []string
	if f.KickSession == "" {
		missing = append(missing, "kick_session")
	}
	if f.SessionToken == "" {
		missing = append(missing, "session_token")
	}
	if len(missing) > 0 {
		return f, fmt.Errorf("missing required cookie(s): %s — make sure you're signed in to kick.com before exporting", strings.Join(missing, ", "))
	}
	return f, nil
}

func isKickDomain(d string) bool {
	d = strings.TrimPrefix(strings.ToLower(d), ".")
	return d == "kick.com" || strings.HasSuffix(d, ".kick.com")
}
```

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/api/ -run TestKickCookiesFromNetscape -v`
Expected: PASS (4 tests).

- [ ] **Step 5: Commit**

```bash
git add internal/api/cookies_netscape.go internal/api/cookies_netscape_test.go
git commit -m "feat(api): Netscape cookies.txt parser for Kick logins"
```

---

### Task 4: Wire parser into the Kick login POST (TDD)

**Files:**
- Modify: `internal/api/handlers_login_kick.go` (`post` method, :65-91)
- Modify: `internal/api/handlers_login_kick_test.go` (add handler test)

- [ ] **Step 1: Write failing handler test**

Append to `internal/api/handlers_login_kick_test.go` (add imports: net/http, net/http/httptest, net/url, strings, github.com/go-chi/chi/v5, github.com/alexedwards/scs/v2):

```go
func TestLoginKickPost_CookiesTxtPersistsSession(t *testing.T) {
	const id = "acc_0123456789abcdef01234567"
	d, ss := newKickLoginDeps(t, id)
	d.sm = scs.New() // post() writes a flash on success

	r := chi.NewRouter()
	r.Post("/accounts/{id}/login", d.post)
	h := d.sm.LoadAndSave(r)

	form := url.Values{"cookies_txt": {cookiesTxtOK}}
	req := httptest.NewRequest(http.MethodPost, "/accounts/"+id+"/login", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusSeeOther {
		t.Fatalf("want 303 redirect, got %d (body %q)", rec.Code, rec.Body.String())
	}
	if _, ok, err := ss.Get(context.Background(), id); err != nil || !ok {
		t.Fatalf("session not persisted: ok=%v err=%v", ok, err)
	}
}
```

- [ ] **Step 2: Run test, verify failure**

Run: `go test ./internal/api/ -run TestLoginKickPost -v`
Expected: FAIL — handler still reads the old 4 form fields, `kick_session` empty → session persists with empty cookies but parser path absent; the assertion that matters is added in Step 3's handler change making `cookies_txt` the input. (If it accidentally passes by persisting empty cookies, the Step 3 rewrite makes the error path explicit — re-run after.)

- [ ] **Step 3: Rewrite post()**

Replace the body of `(d *loginKickDeps) post` in `handlers_login_kick.go`:

```go
func (d *loginKickDeps) post(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	acc, err := d.q.GetAccount(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	// cookies.txt is a few KiB; cap the body well below anything abusive.
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)

	form, err := kickCookiesFromNetscape(r.FormValue("cookies_txt"))
	if err != nil {
		d.renderError(w, r, id, acc.DisplayName, err.Error())
		return
	}
	form.Channels = parseKickChannels(r.FormValue("channel"))

	verified, err := d.persistKickSession(r.Context(), id, form)
	if err != nil {
		d.renderError(w, r, id, acc.DisplayName, "failed to persist session: "+err.Error())
		return
	}

	flash := "Kick cookies persisted — watcher will verify shortly"
	if verified {
		flash = "Kick session verified ✓"
	}
	d.sm.Put(r.Context(), "flash", flash)
	http.Redirect(w, r, "/accounts", http.StatusSeeOther)
}
```

Note: `renderError` needs `d.t`; the error-path unit test would need the full template set, so error paths are covered by the parser tests (Task 3) — the handler just relays `err.Error()`.

- [ ] **Step 4: Run tests, verify pass**

Run: `go test ./internal/api/ -v -run 'TestLoginKickPost|TestKickCookiesFromNetscape'`
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/api/handlers_login_kick.go internal/api/handlers_login_kick_test.go
git commit -m "feat(api): Kick login accepts cookies.txt paste"
```

---

### Task 5: Rewrite the Kick login template

**Files:**
- Modify: `internal/web/templates/login_kick.html` (full rewrite of the `content` block)

- [ ] **Step 1: Replace template content**

Replace the three `settings-card` sections (helper download / manual paste / run from source) with a single cookies.txt card. Keep the shell/ph header and flash block as-is. New body:

```html
{{define "login_kick.html"}}{{template "layout" .}}{{end}}

{{define "title"}}Kick login · GrubDrops{{end}}

{{define "content"}}
{{with .Page}}
<div class="shell narrow">
  <div class="ph">
    <div>
      <div class="kicker">// kick · authorize</div>
      <h1>Authorize {{.DisplayName}}</h1>
    </div>
    <div class="actions"><a class="btn ghost" href="/accounts">← accounts</a></div>
  </div>

  <p class="lede">Kick has no public OAuth, so GrubDrops replays your kick.com session. Export your cookies once with a browser extension and paste them here — when discovery logs cloudflare / 401 the cookies have gone stale; re-export and paste again.</p>

  {{if $.Flash}}<div class="login-flash">{{$.Flash}}</div>{{end}}

  <div class="settings-stack">

    <section class="settings-card">
      <header class="section-h"><h3>cookies.txt</h3></header>
      <ol class="steps">
        <li>Install <a href="https://chromewebstore.google.com/detail/get-cookiestxt-locally/cclelndahbckbenkjhflpdbgdldlbecc" target="_blank" rel="noopener noreferrer">Get cookies.txt LOCALLY</a> (Chrome / Edge / Brave) or <a href="https://addons.mozilla.org/en-US/firefox/addon/cookies-txt/" target="_blank" rel="noopener noreferrer">cookies.txt</a> (Firefox).</li>
        <li>Sign in at <code>kick.com</code>, click the extension's icon and hit <b>Export</b> (current site only).</li>
        <li>Pick the downloaded file below — or open it and paste everything into the box.</li>
      </ol>
      <form method="post" action="/accounts/{{.AccountID}}/login">
        <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
        <label>cookies.txt file <span class="opt">fills the box for you</span>
          <input type="file" id="cookies-file" accept=".txt,text/plain">
        </label>
        <label>contents <span class="req">required</span>
          <textarea name="cookies_txt" rows="8" required autocomplete="off" spellcheck="false" placeholder="# Netscape HTTP Cookie File&#10;.kick.com&#9;TRUE&#9;/&#9;TRUE&#9;…&#9;kick_session&#9;…"></textarea>
        </label>
        <div class="row">
          <a class="back-link" href="/accounts">← cancel</a>
          <button class="btn-linear" type="submit">Authorize →</button>
        </div>
      </form>
      <script>
        document.getElementById('cookies-file').addEventListener('change', async (e) => {
          const f = e.target.files[0];
          if (f) document.querySelector('textarea[name="cookies_txt"]').value = await f.text();
        });
      </script>
    </section>

  </div>
</div>
{{end}}
{{end}}
```

- [ ] **Step 2: Verify template parses + app boots**

Run: `go test ./internal/web/ ./internal/api/ && go build ./...`
Expected: PASS (web package's template-parse test, if present, plus build).

Then boot locally and eyeball the page:
```bash
GRUB_MASTER_KEY=$(head -c32 /dev/urandom | base64) GRUB_DB_PATH=/tmp/grub-dev.db go run ./cmd/miner
# open http://localhost:8080 → create admin → add Kick account → Authorize page
```
Expected: single cookies.txt card, file-pick fills textarea, no /download/helper links anywhere.

- [ ] **Step 3: Commit**

```bash
git add internal/web/templates/login_kick.html
git commit -m "feat(web): Kick authorize page is a single cookies.txt form"
```

---

### Task 6: ghcr.io release workflow

**Files:**
- Create: `.github/workflows/release.yml`

- [ ] **Step 1: Write workflow**

```yaml
name: Release images

on:
  push:
    tags: ["v*"]

permissions:
  contents: read
  packages: write

jobs:
  images:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6

      - uses: docker/setup-buildx-action@v3

      - uses: docker/login-action@v3
        with:
          registry: ghcr.io
          username: ${{ github.actor }}
          password: ${{ secrets.GITHUB_TOKEN }}

      - name: Build + push miner
        uses: docker/build-push-action@v6
        with:
          context: .
          file: deploy/Dockerfile.miner
          platforms: linux/amd64
          push: true
          tags: |
            ghcr.io/aalejandrofer/grubdrops:${{ github.ref_name }}
            ghcr.io/aalejandrofer/grubdrops:latest
          cache-from: type=gha
          cache-to: type=gha,mode=max

      - name: Build + push browser sidecar
        uses: docker/build-push-action@v6
        with:
          context: .
          file: deploy/Dockerfile.browser
          platforms: linux/amd64
          push: true
          tags: |
            ghcr.io/aalejandrofer/grubdrops-browser:${{ github.ref_name }}
            ghcr.io/aalejandrofer/grubdrops-browser:latest
          cache-from: type=gha
          cache-to: type=gha,mode=max
```

- [ ] **Step 2: Validate syntax**

Run: `docker run --rm -v "$PWD":/repo -w /repo rhysd/actionlint:latest .github/workflows/release.yml` (or `actionlint` if installed locally; if neither available, `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/release.yml'))"`).
Expected: no errors.

- [ ] **Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "ci: publish miner + browser images to ghcr.io on tag"
```

(Real verification happens at first `v*` tag push after merge — see final task.)

---

### Task 7: README rewrite (deployment-first)

**Files:**
- Rewrite: `README.md`

- [ ] **Step 1: Replace README content**

Keep the existing logo/badges/screenshot header block (lines 1-22) verbatim, then replace everything after the `---` with:

```markdown
## Quick start

Two published images — the miner and a codec-enabled Chrome sidecar that
earns the Kick watch-time:

```yaml
# compose.yml
services:
  miner:
    image: ghcr.io/aalejandrofer/grubdrops:latest
    restart: unless-stopped
    ports: ["8080:8080"]
    environment:
      GRUB_MASTER_KEY: ${GRUB_MASTER_KEY:?run: head -c32 /dev/urandom | base64}
      GRUB_DB_PATH: /data/miner.db
      GRUB_KICK_BROWSER_WATCH: "1"
    volumes:
      - ./data:/data
      # lets the miner start/stop browser sidecars on demand
      - /var/run/docker.sock:/var/run/docker.sock

  # one per Kick account; name must be grubdrops-browser-<username-slug>
  grubdrops-browser-myuser:
    image: ghcr.io/aalejandrofer/grubdrops-browser:latest
    container_name: grubdrops-browser-myuser
    restart: unless-stopped
    expose: ["9090"]
```

```bash
GRUB_MASTER_KEY=$(head -c32 /dev/urandom | base64) docker compose up -d
# → http://localhost:8080 — first run asks you to create an admin login
```

No Kick accounts (Twitch only)? The miner image alone is enough — drop the
sidecar service, the socket mount and `GRUB_KICK_BROWSER_WATCH`.

The full reference compose (sidecar profiles, OIDC, every knob commented) is
[`deploy/docker-compose.yml`](deploy/docker-compose.yml). Prefer building
yourself? `docker build -f deploy/Dockerfile.miner .` or plain
`go build ./cmd/miner`.

## Add accounts

**Twitch** — official device-code login. Add the account, approve the code at
`twitch.tv/activate`. Your password and cookies never touch GrubDrops.

**Kick** — no public OAuth, so you hand GrubDrops your kick.com session as a
`cookies.txt` export:

1. Install [Get cookies.txt LOCALLY](https://chromewebstore.google.com/detail/get-cookiestxt-locally/cclelndahbckbenkjhflpdbgdldlbecc)
   (Chrome/Edge/Brave) or [cookies.txt](https://addons.mozilla.org/en-US/firefox/addon/cookies-txt/) (Firefox).
2. Sign in at `kick.com`, click the extension icon, **Export** (current site).
3. In GrubDrops: account → **Authorize** → upload or paste the export. Done.

When the cookies go stale (discovery logs cloudflare / 401), re-export and
paste again. Channels auto-discover from each campaign's game — nothing else
to configure.

## What it does

- 🎯 **You set a whitelist** (global or per-account) and nothing outside it ever gets mined.
- 🟣🟢 **Twitch and Kick together**, several accounts each, all on one page.
- ✅ **It checks the game** so you never burn watch-time on the wrong stream.
- 🔗 **It knows about account links** (Krafton, Embark, …) with a per-account "I've linked it" override.
- 🖥️ **A live console**: lifetime stats, current mining, drops catalog, claim history.
- 🔔 **Discord notifications**, toggle per event type.

## How it works

- **Twitch:** device-code login, then GraphQL + PubSub for progress and claims.
- **Kick:** detection and claims ride a Chrome-TLS-fingerprinted HTTP client
  (`utls`) — no Cloudflare dance. Watch-time accrues in an on-demand,
  per-account Chrome sidecar that actually plays the IVS stream; the miner
  starts and stops those containers over the docker socket so Chrome only
  runs while watching.
- **Discovery** sweeps the catalog into SQLite every few minutes.

## Configuration

| Var | Default | Purpose |
|-----|---------|---------|
| `GRUB_MASTER_KEY` | required | Key for the age-encrypted session store. |
| `GRUB_HTTP_ADDR` | `:8080` | Listen address. |
| `GRUB_DB_PATH` | `./miner.db` | SQLite path (use `/data/miner.db` in docker). |
| `GRUB_KICK_BROWSER_WATCH` | `0` | `1` = credit-earning browser watch for Kick (needs sidecar image + socket). |
| `GRUB_KICK_SIDECAR_TEMPLATE` | `grubdrops-browser-{slug}` | Per-account sidecar container name template. |
| `GRUB_KICK_SIDECAR_PORT` | `9090` | Sidecar gRPC port. |
| `GRUB_BROWSER_URL` | none | Fixed sidecar address (legacy always-on mode). |
| `GRUB_DISCOVERY_INTERVAL` | `5m` | Catalog scrape cadence. |
| `GRUB_AUTHCHECK_INTERVAL` | `12h` | Auth health sweep cadence. |
| `GRUB_DISCORD_WEBHOOK_URL` | none | Optional global Discord webhook. |
| `GRUB_SECURE_COOKIES` | `1` | Secure session cookies (turn off for plain-HTTP localhost). |
| `GRUB_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error`. |

### Single sign-on (OIDC)

Optional; password login stays as fallback. Works with any OIDC provider
(authentik, Auth0, Keycloak, Google, Okta, …). SSO turns on when the first
four are set:

| Var | Required | Purpose |
|-----|----------|---------|
| `GRUB_OIDC_ISSUER` | yes | Issuer URL. |
| `GRUB_OIDC_CLIENT_ID` | yes | OAuth client ID. |
| `GRUB_OIDC_CLIENT_SECRET` | yes | OAuth client secret. |
| `GRUB_OIDC_REDIRECT_URL` | yes | `https://<host>/auth/oidc/callback`, registered with the IdP. |
| `GRUB_OIDC_PROVIDER_NAME` | no | Button label (default `SSO`). |
| `GRUB_OIDC_ALLOWED_EMAILS` | no | Comma-separated email allowlist. |
| `GRUB_OIDC_ALLOWED_GROUPS` | no | Required group(s) on the `groups` claim. |

**With no allowlist set, any user the IdP authenticates becomes admin** — scope
membership in the IdP or set an allowlist.

## Pages

| Page | What's on it |
|------|------|
| **Console** (`/`) | Lifetime stats, per-account mining, live event feed. |
| **Drops** (`/drops`) | Past/current/upcoming campaigns, items, connect chips, one-click whitelisting. |
| **History** (`/history`) | Claim log across every account. |
| **Settings** (`/settings`) | Priority list, intervals, Discord, log level, password. |
| **Accounts** | Add accounts, per-account whitelists, re-auth, auth health. |

## Project layout

```
cmd/miner               main daemon
internal/platform/...   per-platform backends (twitch, kick)
internal/watcher        per-account state machine (watch, mine, claim)
internal/dockerctl      on-demand sidecar start/stop over the docker socket
internal/discovery      catalog scraper
internal/api + web      HTMX UI and handlers
internal/store          SQLite (sqlc + goose), age-encrypted sessions
```

## Credits

GrubDrops stands on the shoulders of projects that figured out the hard parts first:

- **[DevilXD/TwitchDropsMiner](https://github.com/DevilXD/TwitchDropsMiner)** — the Twitch device-code flow, GraphQL queries and watch-time mechanics.
- **[HyperBeats/KickDropsMiner](https://github.com/HyperBeats/KickDropsMiner)** — mapped out how Kick drops work in the first place.

GrubDrops is its own Go rewrite with a web UI and multi-account support, but it wouldn't exist without their groundwork. Thank you.

## Notes

Self-hosted, single-tenant, actively developed. `/healthz` for liveness; keep
`/data` across redeploys; put it behind a reverse proxy. Use it responsibly
and within each platform's Terms of Service, against your own accounts, at
your own risk.

---

<sub>Built by <a href="https://github.com/aalejandrofer">@aalejandrofer</a> with <a href="https://claude.com/claude-code">Claude Code</a>. See the <a href="CHANGELOG.md">changelog</a> and <a href="docs/DESIGN.md">design notes</a>.</sub>
```

(Nested fences: the compose block inside README uses standard triple-backtick — when writing the actual file there is no outer fence, so this renders fine.)

- [ ] **Step 2: Verify**

Run: `grep -n "helper\|GRUB_HELPER" README.md`
Expected: no hits. Preview rendering (`gh markdown-preview` or open in editor) — tables + nested code blocks render.

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: deployment-first README — ghcr images, cookies.txt setup"
```

---

### Task 8: Repo tidy + final verification

**Files:**
- Delete: `cmd/kick-encrypt/`
- Modify: `CHANGELOG.md` (new entry at top)

- [ ] **Step 1: Delete kick-encrypt**

```bash
git rm -r cmd/kick-encrypt
go build ./...
```
Expected: build PASS (self-contained one-shot tool, nothing imports it). `cmd/kick-probe` stays — live ops tool.

- [ ] **Step 2: CHANGELOG entry**

Add at top, matching existing entry style:

```markdown
## Unreleased

- **BREAKING:** helper CLI/GUI removed (`cmd/grubdrops-helper*`, `internal/helper`,
  `POST /helper/accounts/{id}/kick`, `GET /download/helper`, `GRUB_HELPER_DIR`).
  Kick cookie ingestion is now a cookies.txt export (browser extension) pasted or
  uploaded on the authorize page; remote users sign in via SSO first.
- Release workflow publishes `ghcr.io/aalejandrofer/grubdrops` and
  `ghcr.io/aalejandrofer/grubdrops-browser` on `v*` tags.
- README rewritten deployment-first; `cmd/kick-encrypt` one-shot tool deleted.
```

- [ ] **Step 3: Full verification sweep**

```bash
go build ./... && go vet ./... && go test ./... -race
gofmt -l . | grep -v '/gen/' || true            # expect empty
grep -rn "grubdrops-helper\|helperIngest\|GRUB_HELPER_DIR\|kooky\|download/helper" \
  --include="*.go" --include="*.html" --include="*.yml" --include="*.yaml" \
  --include="Dockerfile*" --include="*.sh" --include="*.md" \
  cmd/ internal/ deploy/ scripts/ .github/ README.md
docker build -f deploy/Dockerfile.miner -t grubdrops:cleanup-test .
```
Expected: tests green, gofmt clean, grep hits only CHANGELOG.md (historical) and docs/superpowers specs/plans, docker build succeeds without helper stage.

- [ ] **Step 4: Commit**

```bash
git add -A
git commit -m "chore: drop kick-encrypt one-shot tool, changelog for helper removal"
```

---

### Task 9: Branch, PR, post-merge release

(This plan executes on a feature branch — `git checkout -b feat/cookies-txt-cleanup` before Task 1 if not already on one.)

- [ ] **Step 1: Push + PR**

```bash
git push -u origin feat/cookies-txt-cleanup
gh pr create --title "feat!: cookies.txt login, helper removal, ghcr release pipeline" --body "$(cat <<'EOF'
## Summary
- **Helper stack removed** (BREAKING): both CLIs, `internal/helper`, the no-auth
  `POST /helper/accounts/{id}/kick` ingest, `GET /download/helper`, the Dockerfile
  cross-compile stage and `GRUB_HELPER_DIR`. Remote users: SSO login + paste.
- **cookies.txt Kick login:** authorize page is one form — export with the
  "Get cookies.txt LOCALLY" extension, upload or paste; Netscape parser extracts
  `kick_session`/`session_token` (required) + `XSRF-TOKEN`/`cf_clearance`.
- **ghcr release pipeline:** `v*` tag pushes `grubdrops` + `grubdrops-browser` images.
- **README rewritten deployment-first**; `cmd/kick-encrypt` deleted.

## Test plan
- [ ] `go test ./... -race` green, gofmt clean
- [ ] Local boot: Kick authorize page renders, file-pick fills textarea, real
      cookies.txt export persists + verifies a session
- [ ] `docker build -f deploy/Dockerfile.miner .` succeeds (no helper stage)

🤖 Generated with [Claude Code](https://claude.com/claude-code)
EOF
)"
```

- [ ] **Step 2: After merge — tag and watch the release workflow**

```bash
git checkout master && git pull
git tag v0.2.0 && git push origin v0.2.0
gh run watch                      # Release images workflow
```
Expected: both images appear under github packages. Then (separate, manual): flip prod compose to ghcr images per deploy runbook.

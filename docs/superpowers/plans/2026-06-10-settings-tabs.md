# Settings Tabs (split the long settings page) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Split the one long `/settings` page into five subnav tabs — General, Drop Priority, Notifications, Security, Accounts — each its own route rendering only its section.

**Architecture:** Keep a single `settings.html` template that renders exactly one section based on `$.Active`. Add GET handlers per tab (all reuse one data-loader so every page has the data it needs) and split the monolithic settings POST into field-scoped handlers so a partial form can never wipe omitted settings. Routing in `server.go`. Accounts already has its own page.

**Tech Stack:** Go html/template (server-rendered), chi router, scs sessions, `store.Settings`. Tests render templates via `web.Templates()` and assert substrings.

**Tabs / routes:**
| Tab | Active | GET | section content | POST target(s) |
|---|---|---|---|---|
| General | `settings` | `/settings` | tick + discovery intervals, log level, log retention, Status (read-only) | `/settings` (postGeneral) |
| Drop Priority | `priority` | `/settings/priority` | global priority list + add-by-name + priority mode | `/settings/global-games`, `/settings/global-games/add`, `/settings/priority-mode` |
| Notifications | `notifications` | `/settings/notifications` | Discord webhook, avatar, notify kinds, send-test | `/settings/notifications`, `/settings/notify-test` |
| Security | `security` | `/settings/security` | SSO status card (read-only) + master password | `/settings/password` |
| Accounts | `accounts` | `/accounts` | (existing) | (existing) |

---

## File Structure

- `internal/api/handlers_settings.go` — rename `get`→`renderTab(w,r,active)`; add `getPriority`/`getNotifications`/`getSecurity`; split `post`→`postGeneral`+`postNotifications`+`postPriorityMode`; repoint `globalGamesPost`/`globalGamesAdd`/`changePassword` redirects to their tabs.
- `internal/web/templates/settings.html` — 5-link subnav; body sections gated on `$.Active`; drag `<script>` moved inside the priority section.
- `internal/api/server.go` — register the new GET + POST routes.
- `internal/api/handlers_settings_test.go` — per-tab render tests + a field-scope POST test.

---

## Task 1: Split settings handlers (GET loaders + field-scoped POSTs)

**Files:**
- Modify: `internal/api/handlers_settings.go`
- Test: `internal/api/handlers_settings_test.go`

- [ ] **Step 1: Write failing tests**

Add to `internal/api/handlers_settings_test.go`:

```go
func TestPostNotifications_DoesNotTouchIntervals(t *testing.T) {
	st := store.NewMemorySettings() // see Step 3 note if this constructor differs
	_ = st
}
```

> NOTE: the settings store may not have an in-memory constructor. If
> `store.NewMemorySettings` does not exist, DELETE this placeholder test and
> instead rely on the render tests below + the per-handler field scoping (the
> POST handlers only call the setters for their own fields, which is verified
> by reading the code). Do not invent a store constructor.

Add these render tests (these are the real coverage):

```go
func renderSettingsTab(t *testing.T, active string, page settingsPageData) string {
	t.Helper()
	tmpl, err := web.Templates()
	if err != nil {
		t.Fatalf("load templates: %v", err)
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "settings.html", templateData{Active: active, Page: page}); err != nil {
		t.Fatalf("render: %v", err)
	}
	return buf.String()
}

func TestSettingsTabs_SubnavHasFiveLinks(t *testing.T) {
	out := renderSettingsTab(t, "settings", settingsPageData{})
	for _, want := range []string{
		`href="/settings"`, `href="/settings/priority"`,
		`href="/settings/notifications"`, `href="/settings/security"`, `href="/accounts"`,
		"General", "Drop Priority", "Notifications", "Security", "Accounts",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("subnav missing %q", want)
		}
	}
}

func TestSettingsTabs_GeneralSectionOnly(t *testing.T) {
	out := renderSettingsTab(t, "settings", settingsPageData{})
	if !strings.Contains(out, `name="tick_interval_ms"`) {
		t.Errorf("general tab should show interval fields")
	}
	if strings.Contains(out, `action="/settings/global-games"`) {
		t.Errorf("general tab should NOT show the priority list form")
	}
	if strings.Contains(out, `name="discord_webhook"`) {
		t.Errorf("general tab should NOT show notifications")
	}
}

func TestSettingsTabs_PrioritySection(t *testing.T) {
	out := renderSettingsTab(t, "priority", settingsPageData{})
	if !strings.Contains(out, `action="/settings/global-games"`) {
		t.Errorf("priority tab should show the global priority list form")
	}
	if !strings.Contains(out, `name="priority_mode"`) {
		t.Errorf("priority tab should show the priority mode selector")
	}
	if !strings.Contains(out, `action="/settings/priority-mode"`) {
		t.Errorf("priority mode posts to its own endpoint")
	}
}

func TestSettingsTabs_NotificationsSection(t *testing.T) {
	out := renderSettingsTab(t, "notifications", settingsPageData{})
	if !strings.Contains(out, `name="discord_webhook"`) {
		t.Errorf("notifications tab should show the webhook field")
	}
	if !strings.Contains(out, `action="/settings/notifications"`) {
		t.Errorf("notifications form posts to /settings/notifications")
	}
}

func TestSettingsTabs_SecuritySection(t *testing.T) {
	out := renderSettingsTab(t, "security", settingsPageData{OIDC: settingsOIDC{Enabled: false}})
	if !strings.Contains(out, "Single sign-on") {
		t.Errorf("security tab should show the SSO card")
	}
	if !strings.Contains(out, `action="/settings/password"`) {
		t.Errorf("security tab should show the password form")
	}
}
```

Ensure the test file imports `bytes`, `strings`, `testing`, `github.com/aalejandrofer/grubdrops/internal/web` (most already present). Remove the placeholder `TestPostNotifications_DoesNotTouchIntervals` per the NOTE.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestSettingsTabs -v`
Expected: FAIL — the template still renders one combined page (no `/settings/priority` link, etc.), and `/settings/priority-mode` action does not exist.

(These will pass only after Task 2's template change — that is expected; this task lands the handlers, Task 2 the template. Keep the tests; they go green at the end of Task 2. For Task 1's own gate, just ensure the package compiles and existing tests pass after the handler edits.)

- [ ] **Step 3: Rename `get` → `renderTab`, add per-tab GET handlers**

In `internal/api/handlers_settings.go`, change the function signature on the line:
```go
func (d *settingsDeps) get(w http.ResponseWriter, r *http.Request) {
```
to:
```go
func (d *settingsDeps) renderTab(w http.ResponseWriter, r *http.Request, active string) {
```
and inside its `render(...)` call change:
```go
		AuthedAdmin: true, CSRFToken: csrfToken(r), Active: "settings",
```
to:
```go
		AuthedAdmin: true, CSRFToken: csrfToken(r), Active: active,
```

Immediately after the closing `}` of `renderTab`, add the four thin GET wrappers:
```go
func (d *settingsDeps) get(w http.ResponseWriter, r *http.Request)              { d.renderTab(w, r, "settings") }
func (d *settingsDeps) getPriority(w http.ResponseWriter, r *http.Request)      { d.renderTab(w, r, "priority") }
func (d *settingsDeps) getNotifications(w http.ResponseWriter, r *http.Request) { d.renderTab(w, r, "notifications") }
func (d *settingsDeps) getSecurity(w http.ResponseWriter, r *http.Request)      { d.renderTab(w, r, "security") }
```

- [ ] **Step 4: Split the monolithic `post` into field-scoped handlers**

Replace the entire `post` function (from `func (d *settingsDeps) post(` through its closing `}` ending with the `http.Redirect(w, r, "/settings", ...)`) with these three handlers:

```go
// postGeneral saves the General tab: tick/discovery intervals + logging.
func (d *settingsDeps) postGeneral(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if v := r.FormValue("log_retention_days"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			_ = d.s.SetLogRetentionDays(ctx, n)
		}
	}
	_ = d.s.SetLogLevel(ctx, r.FormValue("log_level"))

	intervalsChanged := false
	if v := r.FormValue("tick_interval_ms"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if cur, _ := d.s.TickIntervalMs(ctx); cur != n {
				intervalsChanged = true
			}
			_ = d.s.SetTickIntervalMs(ctx, n)
		}
	}
	if v := r.FormValue("discovery_interval_sec"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			if cur, _ := d.s.DiscoveryIntervalSec(ctx); cur != n {
				intervalsChanged = true
			}
			_ = d.s.SetDiscoveryIntervalSec(ctx, n)
		}
	}
	if d.onUpdate != nil {
		d.onUpdate()
	}
	msg := "settings saved"
	if intervalsChanged {
		msg = "settings saved — restart container to apply the new tick/discovery interval"
	}
	d.sm.Put(ctx, "flash", msg)
	http.Redirect(w, r, "/settings", http.StatusSeeOther)
}

// postNotifications saves the Notifications tab: webhook, avatar, notify kinds.
func (d *settingsDeps) postNotifications(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := d.s.SetGlobalDiscordWebhook(ctx, r.FormValue("discord_webhook")); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = d.s.SetNotifyAvatarURL(ctx, strings.TrimSpace(r.FormValue("notify_avatar_url")))
	on := func(name string) bool { return r.FormValue(name) == "1" }
	_ = d.s.SetNotifyKinds(ctx, on("notify_claim"), on("notify_progress"), on("notify_auth"), on("notify_error"))
	if d.onUpdate != nil {
		d.onUpdate()
	}
	d.sm.Put(ctx, "flash", "notifications saved")
	http.Redirect(w, r, "/settings/notifications", http.StatusSeeOther)
}

// postPriorityMode saves the Drop Priority tab's mode selector.
func (d *settingsDeps) postPriorityMode(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	_ = d.s.SetPriorityMode(ctx, r.FormValue("priority_mode"))
	if d.onUpdate != nil {
		d.onUpdate()
	}
	d.sm.Put(ctx, "flash", "priority mode saved")
	http.Redirect(w, r, "/settings/priority", http.StatusSeeOther)
}
```

- [ ] **Step 5: Repoint the existing handlers' redirects to their tabs**

In `globalGamesPost`: change BOTH `http.Redirect(w, r, "/settings", ...)` lines (the early-return and the final) to `"/settings/priority"`.

In `globalGamesAdd`: change ALL `http.Redirect(w, r, "/settings", ...)` lines to `"/settings/priority"`.

In `changePassword`: change BOTH `http.Redirect(w, r, "/settings", ...)` lines to `"/settings/security"`.

(Leave `notifyTest` alone — it returns an HTMX fragment, no redirect.)

- [ ] **Step 6: Build + existing tests**

Run: `go build ./... && go test ./internal/api/ -run "TestSettings_SSOCard|TestSettings$" -v`
Expected: build clean; existing settings render test still passes (it passes `templateData{Page:...}` with no Active → renders the General section, which is fine — see Task 2 default).

- [ ] **Step 7: Commit**

```bash
git add internal/api/handlers_settings.go internal/api/handlers_settings_test.go
git commit -m "feat(settings): per-tab GET loaders + field-scoped POST handlers"
```

---

## Task 2: Restructure settings.html into tabbed sections

**Files:**
- Modify: `internal/web/templates/settings.html`

- [ ] **Step 1: Replace the subnav**

In `internal/web/templates/settings.html`, replace the `<nav class="subnav">…</nav>` block (the General/Accounts one) with:

```html
  <nav class="subnav">
    <a href="/settings" class="subnav-link {{if eq .Active "settings"}}on{{end}}">General</a>
    <a href="/settings/priority" class="subnav-link {{if eq .Active "priority"}}on{{end}}">Drop Priority</a>
    <a href="/settings/notifications" class="subnav-link {{if eq .Active "notifications"}}on{{end}}">Notifications</a>
    <a href="/settings/security" class="subnav-link {{if eq .Active "security"}}on{{end}}">Security</a>
    <a href="/accounts" class="subnav-link {{if eq .Active "accounts"}}on{{end}}">Accounts</a>
  </nav>
```

- [ ] **Step 2: Gate each section by `$.Active` and split the combined form**

Inside `{{with .Page}}`/`<div class="settings-stack">`, restructure so each section is wrapped in an `{{if eq $.Active "..."}}` guard. The General tab is the default (also renders when `$.Active` is empty, so the existing no-Active render test still works).

Replace the whole `<div class="settings-stack"> … </div>` body with:

```html
  <div class="settings-stack">

    {{if eq $.Active "priority"}}
    <section class="settings-card">
      <header class="section-h"><h3>Global priority list</h3><span class="meta">Fallback when account list empty</span></header>
      <form method="post" action="/settings/global-games" id="global-games-form">
        <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
        <div class="settings-grid-2">
          <div>
            <div class="kicker">Selected · drag to reorder</div>
            <ul id="global-selected" class="rank-list">
              {{range .GlobalGames}}
              <li draggable="true" data-game-id="{{.ID}}">
                <span>{{.Name}}</span>
                <button type="button" class="rank-rm" onclick="gpRemove('{{.ID}}')">remove ×</button>
              </li>
              {{end}}
              {{if not .GlobalGames}}
              <li class="rank-empty">No global priority set — pick from the right →</li>
              {{end}}
            </ul>
          </div>
          <div>
            <div class="kicker">Available</div>
            <ul id="global-available" class="rank-list rank-available">
              {{range .AllGames}}
              {{if not .Selected}}
              <li data-game-id="{{.ID}}">
                <span>{{.Name}}</span>
                <button type="button" class="rank-add" onclick="gpAdd('{{.ID}}','{{.Name}}')">add +</button>
              </li>
              {{end}}
              {{end}}
            </ul>
          </div>
        </div>
        <div class="row">
          <span class="hint">Order in the left column = mining priority for accounts without an override.</span>
          <button class="btn primary" type="submit">Save priority →</button>
        </div>
      </form>

      <form method="post" action="/settings/global-games/add" class="add-by-name">
        <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
        <span class="kicker">Add by name</span>
        <input type="text" name="name" placeholder="e.g. Marathon" required>
        <button class="btn" type="submit">Add →</button>
        <span class="hint">Seed the list before scrape surfaces it.</span>
      </form>
    </section>

    <section class="settings-card">
      <header class="section-h"><h3>Drop priority mode</h3><span class="meta">How accounts pick the next campaign</span></header>
      <form method="post" action="/settings/priority-mode">
        <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
        <label>Priority mode
          <select name="priority_mode">
            <option value="ordered" {{if eq .PriorityMode "ordered"}}selected{{end}}>Ordered — top whitelist first</option>
            <option value="ending_soonest" {{if eq .PriorityMode "ending_soonest"}}selected{{end}}>Ending soonest — claim expiring first</option>
          </select>
        </label>
        <div class="row">
          <span class="hint">Applies live to all accounts.</span>
          <button class="btn primary" type="submit">Save →</button>
        </div>
      </form>
    </section>
    {{end}}

    {{if eq $.Active "notifications"}}
    <section class="settings-card">
      <header class="section-h"><h3>Notifications</h3><span class="meta">Discord</span></header>
      <form method="post" action="/settings/notifications" id="notifications-form">
        <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
        <label>Global Discord webhook URL
          <input type="url" name="discord_webhook" value="{{.GlobalDiscordWebhook}}" placeholder="https://discord.com/api/webhooks/…">
        </label>
        <label>Bot avatar URL <span class="field-hint" style="margin:0;font-weight:400;">optional — image shown as the sender avatar</span>
          <input type="url" name="notify_avatar_url" value="{{.NotifyAvatarURL}}" placeholder="https://…/avatar.png">
        </label>
        <div class="settings-grid-4">
          <label class="cb"><input type="checkbox" name="notify_claim" value="1" {{if .NotifyClaim}}checked{{end}}> claims</label>
          <label class="cb"><input type="checkbox" name="notify_progress" value="1" {{if .NotifyProgress}}checked{{end}}> progress</label>
          <label class="cb"><input type="checkbox" name="notify_auth" value="1" {{if .NotifyAuth}}checked{{end}}> auth</label>
          <label class="cb"><input type="checkbox" name="notify_error" value="1" {{if .NotifyError}}checked{{end}}> errors</label>
        </div>
        <div class="row" style="margin-top:10px;align-items:center;gap:12px;">
          <button class="btn" type="button"
            hx-post="/settings/notify-test"
            hx-target="#notify-test-result"
            hx-swap="innerHTML"
            hx-headers='{"X-CSRF-Token": "{{$.CSRFToken}}"}'>Send test →</button>
          <span id="notify-test-result"></span>
        </div>
        <div class="field-hint" style="margin:6px 0 0;">Sends a sample embed to your webhook (global, or the first account webhook). Save the webhook first.</div>
        <div class="row">
          <a class="back-link" href="/">← back</a>
          <button class="btn primary" type="submit">Save →</button>
        </div>
      </form>
    </section>
    {{end}}

    {{if eq $.Active "security"}}
    <section class="settings-card">
      <header class="section-h">
        <h3>Single sign-on (SSO)</h3>
        {{if .OIDC.Enabled}}
          <span class="sso-status-pill on">● enabled</span>
        {{else}}
          <span class="sso-status-pill off">○ disabled</span>
        {{end}}
      </header>
      {{if .OIDC.Enabled}}
      <div class="kvlist">
        <div class="kvrow"><span class="k">provider</span><span class="v">{{.OIDC.ProviderName}}</span></div>
        <div class="kvrow"><span class="k">issuer</span><span class="v">{{.OIDC.Issuer}}</span></div>
        <div class="kvrow"><span class="k">access</span><span class="v">
          {{if or .OIDC.AllowedEmails .OIDC.AllowedGroups}}
            {{range .OIDC.AllowedEmails}}{{.}} {{end}}{{range .OIDC.AllowedGroups}}@{{.}} {{end}}
          {{else}}Any user authenticated by your identity provider{{end}}
        </span></div>
      </div>
      <div class="kvrow" style="margin-top:10px;"><span class="k">callback</span></div>
      <div class="copy-field">
        <code id="sso-callback">{{.OIDC.CallbackURL}}</code>
        <button type="button" onclick="navigator.clipboard.writeText(document.getElementById('sso-callback').textContent.trim())">copy</button>
      </div>
      <p class="hint" style="margin-top:10px;">Configured via <code>GRUB_OIDC_*</code> environment variables (read-only here).</p>
      {{else}}
      <p class="hint">Not configured — set the <code>GRUB_OIDC_*</code> environment variables to enable SSO. See the README "Single sign-on (OIDC)" section.</p>
      {{end}}
    </section>

    <section class="settings-card">
      <header class="section-h"><h3>Master password</h3><span class="meta">Admin login</span></header>
      <form method="post" action="/settings/password" autocomplete="off">
        <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
        <label>Current password
          <input type="password" name="current_password" required autocomplete="current-password">
        </label>
        <div class="settings-grid-2">
          <label>New password
            <input type="password" name="new_password" required minlength="8" autocomplete="new-password">
          </label>
          <label>Confirm new password
            <input type="password" name="confirm_password" required minlength="8" autocomplete="new-password">
          </label>
        </div>
        <div class="row">
          <span class="field-hint" style="margin:0;">Minimum 8 characters. You stay logged in after changing it.</span>
          <button class="btn primary" type="submit">Change password →</button>
        </div>
      </form>
    </section>
    {{end}}

    {{if or (eq $.Active "settings") (eq $.Active "")}}
    <section class="settings-card">
      <header class="section-h"><h3>Runtime</h3><span class="meta">Reload required for tick/discovery</span></header>
      <form method="post" action="/settings" id="settings-form">
        <input type="hidden" name="csrf_token" value="{{$.CSRFToken}}">
        <div class="settings-grid-2">
          <label>Watcher tick interval (ms)
            <input type="number" name="tick_interval_ms" value="{{.TickIntervalMs}}" min="100" max="60000">
          </label>
          <label>Discovery interval (seconds)
            <input type="number" name="discovery_interval_sec" value="{{.DiscoveryIntervalSec}}" min="30" max="3600">
          </label>
        </div>
        <div class="settings-grid-2">
          <label>Log level
            <select name="log_level">
              <option value="" {{if eq .LogLevel ""}}selected{{end}}>default ({{.LogLevelEnv}})</option>
              <option value="debug" {{if eq .LogLevel "debug"}}selected{{end}}>debug</option>
              <option value="info" {{if eq .LogLevel "info"}}selected{{end}}>info</option>
              <option value="warn" {{if eq .LogLevel "warn"}}selected{{end}}>warn</option>
              <option value="error" {{if eq .LogLevel "error"}}selected{{end}}>error</option>
            </select>
          </label>
          <label>Log retention (days)
            <input type="number" name="log_retention_days" value="{{.LogRetentionDays}}" min="1" max="365">
          </label>
        </div>
        <div class="row">
          <a class="back-link" href="/">← back</a>
          <button class="btn primary" type="submit">Save →</button>
        </div>
      </form>
    </section>

    <section class="settings-card">
      <header class="section-h"><h3>Status</h3><span class="meta">Read-only</span></header>
      <div class="kvlist">
        <div class="kvrow"><span class="k">uptime</span><span class="v">{{if .Uptime}}{{.Uptime}}{{else}}—{{end}}</span></div>
        <div class="kvrow"><span class="k">go</span><span class="v">{{.GoVersion}}</span></div>
        <div class="kvrow"><span class="k">goroutines</span><span class="v">{{.Goroutines}}</span></div>
        <div class="kvrow"><span class="k">sidecar</span><span class="v">{{if .BrowserURL}}{{.BrowserURL}}{{else}}—{{end}}</span></div>
        {{if .GitCommit}}<div class="kvrow"><span class="k">commit</span><span class="v">{{.GitCommit}}</span></div>{{end}}
        {{if .Version}}<div class="kvrow"><span class="k">version</span><span class="v">{{.Version}}</span></div>{{end}}
      </div>
    </section>
    {{end}}

  </div>
```

- [ ] **Step 2b: Move the drag `<script>` so it only loads on the priority tab**

The existing `<script>…</script>` block (the global-games drag/reorder JS) is after the `{{end}}` of `{{with .Page}}`. Wrap it in a guard so it only renders on the priority tab. Change the bare `<script>` opening to be preceded by `{{if eq .Active "priority"}}` and add `{{end}}` after its closing `</script>`:

```html
{{if eq .Active "priority"}}
<script>
... (unchanged drag JS) ...
</script>
{{end}}
```

(The JS already guards `if (!sel || !form) return;`, so leaving it everywhere would be harmless — but gating it keeps other tabs clean.)

- [ ] **Step 3: Run the tab render tests**

Run: `go test ./internal/api/ -run "TestSettingsTabs|TestSettings_SSOCard" -v`
Expected: PASS (all the Task 1 tab tests now go green with the template in place).

- [ ] **Step 4: Build + full api tests**

Run: `go build ./... && go test ./internal/api/ -v 2>&1 | tail -15`
Expected: build clean, all PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/web/templates/settings.html
git commit -m "feat(settings): tabbed sections rendered by active tab"
```

---

## Task 3: Wire routes + redeploy

**Files:**
- Modify: `internal/api/server.go`

- [ ] **Step 1: Register the new routes**

In `internal/api/server.go`, find the settings route block:
```go
	authed.Get("/settings", settingsH.get)
	authed.Post("/settings", settingsH.post)
	authed.Post("/settings/global-games", settingsH.globalGamesPost)
	authed.Post("/settings/global-games/add", settingsH.globalGamesAdd)
	authed.Post("/settings/password", settingsH.changePassword)
	authed.Post("/settings/notify-test", settingsH.notifyTest)
```
Replace it with:
```go
	authed.Get("/settings", settingsH.get)
	authed.Get("/settings/priority", settingsH.getPriority)
	authed.Get("/settings/notifications", settingsH.getNotifications)
	authed.Get("/settings/security", settingsH.getSecurity)
	authed.Post("/settings", settingsH.postGeneral)
	authed.Post("/settings/priority-mode", settingsH.postPriorityMode)
	authed.Post("/settings/notifications", settingsH.postNotifications)
	authed.Post("/settings/global-games", settingsH.globalGamesPost)
	authed.Post("/settings/global-games/add", settingsH.globalGamesAdd)
	authed.Post("/settings/password", settingsH.changePassword)
	authed.Post("/settings/notify-test", settingsH.notifyTest)
```

- [ ] **Step 2: Build, vet, full test**

Run: `go build ./... && go vet ./... && go test ./...`
Expected: all PASS. (`settingsH.post` no longer exists — confirm nothing else references it; the only caller was this route, now `postGeneral`.)

- [ ] **Step 3: Commit**

```bash
git add internal/api/server.go
git commit -m "feat(settings): routes for priority/notifications/security tabs"
```

- [ ] **Step 4: Redeploy + smoke**

```bash
docker buildx build --platform linux/amd64 -f deploy/Dockerfile.miner -t grubdrops:latest --load .
docker save grubdrops:latest | ssh 10.10.2.40 'docker load'
ssh 10.10.2.40 'cd ~/projects/homelab/humblewhale/grubdrops && docker compose up -d'
```
Then verify each tab is reachable (302 to /login is fine if unauthenticated — what matters is no 404/500):
```bash
ssh 10.10.2.40 'for p in /settings /settings/priority /settings/notifications /settings/security; do printf "%s " "$p"; curl -sk -o /dev/null -w "%{http_code}\n" https://drops.ryuzec.dev$p; done'
```
Expected: each returns 200 or 303 (not 404/500). Manually confirm in a browser: the 5-pill subnav, each tab shows only its section, saving on one tab doesn't blank another's settings.

---

## Self-Review Notes

- **Spec coverage:** 5 tabs (General/Drop Priority/Notifications/Security/Accounts) via separate routes (Task 1 GET wrappers + Task 3 routes); priority mode lives under Drop Priority and the tab is named "Drop Priority" (Task 2 subnav + section); data-loss avoided by field-scoped POSTs (Task 1 Step 4) + per-tab redirects (Step 5); Status folded into General (Task 2 General guard); SSO + password under Security (Task 2). Accounts unchanged.
- **Type/route consistency:** GET handlers `get`/`getPriority`/`getNotifications`/`getSecurity` and POST handlers `postGeneral`/`postNotifications`/`postPriorityMode` defined in Task 1 are the exact names wired in Task 3. `renderTab(w,r,active)` is the shared loader. Active values `settings`/`priority`/`notifications`/`security`/`accounts` match between subnav (Task 2) and handlers (Task 1). Form actions in Task 2 (`/settings`, `/settings/priority-mode`, `/settings/notifications`, `/settings/global-games`, `/settings/password`) match the routes in Task 3.
- **Placeholder scan:** the only placeholder is the explicitly-deleted `TestPostNotifications_DoesNotTouchIntervals` stub (Task 1 Step 1 NOTE tells the implementer to remove it rather than invent a store constructor). No other TBDs.
- **Default-tab safety:** the General section guard is `{{if or (eq $.Active "settings") (eq $.Active "")}}` so the pre-existing settings render test (which passes no Active) still renders General and stays green.
```

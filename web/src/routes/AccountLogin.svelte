<script lang="ts">
  import { onMount } from 'svelte';
  import { currentPath, navigate } from '../lib/router.svelte';
  import { apiFetch, apiSend, ApiError } from '../lib/api';

  const id = currentPath().split('/')[2];

  let platform = $state('');
  let displayName = $state('');
  let loading = $state(true);
  let error = $state<string | null>(null);

  // Kick form state
  let cookiesTxt = $state('');
  let channel = $state('');
  let busy = $state(false);
  let kickError = $state<string | null>(null);

  onMount(async () => {
    try {
      const acc = await apiFetch<{ Platform: string; DisplayName: string }>('/api/accounts/' + id);
      platform = acc.Platform;
      displayName = acc.DisplayName;
    } catch (e) {
      error = e instanceof ApiError ? e.message : 'Failed to load account';
    } finally {
      loading = false;
    }
  });

  async function handleFileChange(e: Event) {
    const input = e.target as HTMLInputElement;
    const file = input.files?.[0];
    if (file) cookiesTxt = await file.text();
  }

  async function submitKick() {
    kickError = null;
    busy = true;
    try {
      await apiSend<{ ok: boolean; verified: boolean }>('/api/accounts/' + id + '/kick/login', 'POST', {
        cookies_txt: cookiesTxt,
        channel,
      });
      navigate('/accounts');
    } catch (e) {
      kickError = e instanceof ApiError ? e.message : 'Login failed';
    } finally {
      busy = false;
    }
  }
</script>

{#if loading}
  <div class="auth-wrap"><div class="auth-card"><p>Loading…</p></div></div>
{:else if error}
  <div class="auth-wrap"><div class="auth-card"><div class="err">{error}</div></div></div>
{:else if platform === 'twitch'}
  <div class="auth-wrap wide">
    <div class="auth-card">
      <div class="kicker">// twitch · authorize</div>
      <h1>Authorize {displayName}</h1>
      <p class="intro">Choose how you want to authenticate with Twitch.</p>
    </div>

    <div class="auth-card method preferred">
      <div class="method-head">
        <span class="method-tag">recommended</span>
        <h2>Device-code login</h2>
      </div>
      <p>Mints an Android-issued OAuth token via Twitch's official device-code flow. The most reliable method.</p>
      <div class="method-cta">
        <button class="btn primary" type="button" onclick={() => navigate('/accounts/' + id + '/twitch/device')}>
          Start device-code →
        </button>
        <span class="hint">Opens activate.twitch.tv with a 6-character code. ~30 seconds.</span>
      </div>
    </div>

    <div class="auth-card method">
      <div class="method-head">
        <span class="method-tag muted">migrate</span>
        <h2>Migrate from TwitchDropsMiner</h2>
      </div>
      <p>Already on TwitchDropsMiner?<br>Upload your existing cookies.jar, no new login.</p>
      <div class="method-cta">
        <button class="btn primary" type="button" onclick={() => navigate('/accounts/' + id + '/twitch/cookie')}>
          Import cookies.jar →
        </button>
        <span class="hint">Upload the cookies.jar from your TwitchDropsMiner data folder.</span>
      </div>
    </div>

    <div class="submit-row solo">
      <button class="alt" type="button" onclick={() => navigate('/accounts')}>cancel</button>
    </div>
  </div>
{:else}
  <!-- Kick: cookies.txt paste/upload form -->
  <div class="auth-wrap wide">
    <div class="auth-card">
      <div class="kicker">// kick · authorize</div>
      <h1>Authorize {displayName}</h1>
      <p class="intro">Kick has no public OAuth, so GrubDrops replays your kick.com session. Export your cookies once with a browser extension and paste them here — when discovery logs cloudflare / 401 the cookies have gone stale; re-export and paste again.</p>
    </div>

    {#if kickError}<div class="auth-flash"><div class="err">{kickError}</div></div>{/if}

    <div class="auth-card">
      <form onsubmit={(e) => { e.preventDefault(); submitKick(); }}>
        <div class="method-head">
          <h2>cookies.txt</h2>
        </div>
        <ol class="steps">
          <li>Install <a href="https://chromewebstore.google.com/detail/get-cookiestxt-locally/cclelndahbckbenkjhflpdbgdldlbecc" target="_blank" rel="noopener noreferrer">Get cookies.txt LOCALLY</a> (Chrome / Edge / Brave) or <a href="https://addons.mozilla.org/en-US/firefox/addon/cookies-txt/" target="_blank" rel="noopener noreferrer">cookies.txt</a> (Firefox).</li>
          <li>Sign in at <code>kick.com</code>, click the extension's icon and hit <b>Export</b> (current site only).</li>
          <li>Pick the downloaded file below — or open it and paste everything into the box.</li>
        </ol>
        <label>cookies.txt file <span class="opt">(fills the box for you)</span>
          <input type="file" id="cookies-file" accept=".txt,text/plain" onchange={handleFileChange} />
        </label>
        <label>contents <span class="req">(required)</span>
          <textarea name="cookies_txt" rows="8" required autocomplete="off" spellcheck="false"
            placeholder="# Netscape HTTP Cookie File&#10;.kick.com&#9;TRUE&#9;/&#9;TRUE&#9;…&#9;kick_session&#9;…"
            bind:value={cookiesTxt}></textarea>
        </label>
        <label>Channel (optional)
          <input type="text" bind:value={channel} placeholder="channel name" />
        </label>
        <div class="submit-row">
          <button class="alt" type="button" onclick={() => navigate('/accounts')}>← cancel</button>
          <button class="btn-linear" type="submit" disabled={busy || !cookiesTxt.trim()}>Authorize →</button>
        </div>
      </form>
    </div>
  </div>
{/if}

<script lang="ts">
	import Ico from './_Ico.svelte';

	let { open = $bindable(false) }: { open?: boolean } = $props();

	let dlg = $state<HTMLDialogElement | null>(null);
	let copied = $state('');

	const origin = typeof window === 'undefined' ? 'https://drops.example.com' : window.location.origin;
	const redirect = `${origin}/auth/oidc/callback`;
	const env = `GRUB_OIDC_ISSUER=https://auth.example.com/application/o/grubdrops/
GRUB_OIDC_CLIENT_ID=
GRUB_OIDC_CLIENT_SECRET=
GRUB_OIDC_REDIRECT_URL=${redirect}
GRUB_OIDC_PROVIDER_NAME=authentik
GRUB_OIDC_ALLOWED_EMAILS=you@example.com`;

	$effect(() => {
		if (!dlg) return;
		if (open && !dlg.open) {
			dlg.showModal();
			// Start on the title, not the X, so no focus ring flashes on open.
			dlg.querySelector<HTMLElement>('#oidc-h')?.focus();
		}
		if (!open && dlg.open) dlg.close();
	});

	function copy(key: string, text: string): void {
		void navigator.clipboard?.writeText(text);
		copied = key;
		setTimeout(() => (copied = copied === key ? '' : copied), 1500);
	}
</script>

<!-- A click on the dialog itself (not its card) is the backdrop: close. Escape closes natively. -->
<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<dialog
	bind:this={dlg}
	class="oidc hue-indigo"
	aria-labelledby="oidc-h"
	onclose={() => (open = false)}
	onclick={(e) => e.target === dlg && (open = false)}
>
	<div class="card">
		<header>
			<span class="tile"><Ico name="lock" size={16} /></span>
			<h2 id="oidc-h" tabindex="-1">Set up single sign-on</h2>
			<button type="button" class="x" aria-label="Close" onclick={() => (open = false)}><Ico name="x" size={16} /></button>
		</header>

		<ol>
			<li>
				<b>Create an OIDC app</b> in your provider. Use this redirect URL:
				<div class="copyrow">
					<code>{redirect}</code>
					<button type="button" class="btn ghost" onclick={() => copy('r', redirect)}>
						<Ico name={copied === 'r' ? 'check' : 'copy'} size={14} />{copied === 'r' ? 'Copied' : 'Copy'}
					</button>
				</div>
			</li>
			<li>
				<b>Add these to your env</b> and fill in the ID and secret.
				<div class="code">
					<pre>{env}</pre>
					<button type="button" class="btn ghost" onclick={() => copy('e', env)}>
						<Ico name={copied === 'e' ? 'check' : 'copy'} size={14} />{copied === 'e' ? 'Copied' : 'Copy'}
					</button>
				</div>
			</li>
			<li>
				<b>Restart</b> with <code>docker compose up -d</code>. A Sign in with authentik button shows on the login page.
			</li>
		</ol>

		<p class="warn"><Ico name="alert" size={14} /> Without an email or group allowlist, anyone your provider signs in is admin.</p>

		<footer>
			<a class="btn ghost" href="https://github.com/aalejandrofer/GrubDrops#single-sign-on-oidc" target="_blank" rel="noreferrer"
				>Full guide</a
			>
			<button type="button" class="btn primary" onclick={() => (open = false)}>Done</button>
		</footer>
	</div>
</dialog>

<style>
	dialog.oidc {
		padding: 0;
		border: none;
		background: transparent;
		color: var(--text);
		width: min(560px, calc(100% - 32px));
		max-height: calc(100% - 32px);
		margin: auto;
	}
	dialog.oidc::backdrop {
		background: rgb(0 0 0 / 0.55);
	}
	.card {
		background: var(--surface);
		border: 1px solid var(--border-soft);
		border-radius: 14px;
		padding: 18px 20px 16px;
		box-shadow: 0 20px 60px rgb(0 0 0 / 0.45);
	}
	header {
		display: flex;
		align-items: center;
		gap: 10px;
		margin-bottom: 14px;
	}
	h2 {
		margin: 0;
		font-size: 16px;
		font-weight: 650;
		outline: none;
	}
	.tile {
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		border-radius: 8px;
		color: var(--hue);
		background: color-mix(in srgb, var(--hue) 16%, transparent);
	}
	.x {
		margin-left: auto;
		display: grid;
		place-items: center;
		width: 36px;
		height: 36px;
		margin-right: -6px;
		border: none;
		border-radius: 8px;
		background: none;
		color: var(--t2);
		cursor: pointer;
	}
	.x:focus-visible {
		outline: 2px solid var(--hue);
		outline-offset: 1px;
	}
	.x:hover {
		background: var(--surface-3);
		color: var(--text);
	}
	ol {
		margin: 0;
		padding: 0;
		list-style: none;
		counter-reset: step;
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		gap: 14px;
	}
	li {
		counter-increment: step;
		position: relative;
		padding-left: 32px;
		font-size: 13px;
		line-height: 1.5;
		color: var(--t2);
	}
	li b {
		color: var(--text);
		font-weight: 600;
	}
	li::before {
		content: counter(step);
		position: absolute;
		left: 0;
		top: 0;
		width: 22px;
		height: 22px;
		border-radius: 50%;
		display: grid;
		place-items: center;
		font-size: 12px;
		font-weight: 650;
		color: var(--hue);
		background: color-mix(in srgb, var(--hue) 16%, transparent);
	}
	code,
	pre {
		font-family: var(--mono, ui-monospace, monospace);
		font-size: 12px;
	}
	li > code {
		padding: 1px 5px;
		border-radius: 5px;
		background: var(--surface-3);
		color: var(--text);
	}
	.copyrow,
	.code {
		display: flex;
		align-items: flex-start;
		gap: 8px;
		margin-top: 6px;
		padding: 8px 8px 8px 12px;
		border: 1px solid var(--border-soft);
		border-radius: 9px;
		background: var(--bg);
	}
	.copyrow {
		align-items: center;
	}
	.copyrow code {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
		color: var(--text);
	}
	.code pre {
		flex: 1;
		min-width: 0;
		margin: 0;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		line-height: 1.6;
		color: var(--text);
	}
	.warn {
		display: flex;
		align-items: flex-start;
		gap: 8px;
		margin: 16px 0 0;
		padding: 9px 12px;
		border-radius: 9px;
		font-size: 12.5px;
		color: var(--amber);
		background: var(--amber-soft);
	}
	/* Phone: code gets the full width, Copy sits under it. */
	@media (max-width: 480px) {
		.card {
			padding: 16px;
		}
		li {
			padding-left: 30px;
		}
		.copyrow,
		.code {
			flex-direction: column;
			align-items: stretch;
		}
		.copyrow .btn,
		.code .btn {
			align-self: flex-end;
		}
	}
	footer {
		display: flex;
		justify-content: flex-end;
		gap: 8px;
		margin-top: 16px;
	}
</style>

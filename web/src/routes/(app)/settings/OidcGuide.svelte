<script lang="ts">
	import { onDestroy } from 'svelte';
	import Icon from '$lib/components/Icon.svelte';
	import { t } from '$lib/i18n/index.svelte';
	import { SECTION_LOOK } from '$lib/settings-look';

	let { open = $bindable(false) }: { open?: boolean } = $props();

	const GUIDE_URL = 'https://github.com/aalejandrofer/GrubDrops#single-sign-on-oidc';
	const RESTART = 'docker compose up -d';

	let dlg = $state<HTMLDialogElement | null>(null);
	let copied = $state('');
	let timer: ReturnType<typeof setTimeout> | undefined;

	const redirect = typeof location === 'undefined' ? '/auth/oidc/callback' : `${location.origin}/auth/oidc/callback`;
	/** Env var names and values are config, not prose: never translated. */
	const env = $derived(`GRUB_OIDC_ISSUER=https://auth.example.com/application/o/grubdrops/
GRUB_OIDC_CLIENT_ID=
GRUB_OIDC_CLIENT_SECRET=
GRUB_OIDC_REDIRECT_URL=${redirect}
GRUB_OIDC_PROVIDER_NAME=authentik
GRUB_OIDC_ALLOWED_EMAILS=you@example.com
GRUB_OIDC_ALLOWED_GROUPS=grubdrops-admins`);

	$effect(() => {
		if (!dlg) return;
		if (open && !dlg.open) {
			dlg.showModal();
			// Start on the title, not the X, so no focus ring flashes on open.
			dlg.querySelector<HTMLElement>('#oidc-h')?.focus();
		}
		if (!open && dlg.open) dlg.close();
	});

	/** The page behind a modal is inert, so the plain-HTTP fallback selects text inside the dialog. */
	function legacyCopy(text: string): boolean {
		if (!dlg) return false;
		const ta = document.createElement('textarea');
		ta.value = text;
		ta.style.position = 'fixed';
		ta.style.opacity = '0';
		dlg.appendChild(ta);
		ta.select();
		try {
			return document.execCommand('copy');
		} catch {
			return false;
		} finally {
			ta.remove();
		}
	}

	async function copy(key: string, text: string): Promise<void> {
		let ok = false;
		try {
			await navigator.clipboard.writeText(text);
			ok = true;
		} catch {
			ok = legacyCopy(text);
		}
		if (!ok) return;
		copied = key;
		clearTimeout(timer);
		timer = setTimeout(() => (copied = ''), 2000);
	}

	onDestroy(() => clearTimeout(timer));
</script>

<!-- A click on the dialog itself, outside its card, is the backdrop: close. Escape closes natively. -->
<!-- svelte-ignore a11y_click_events_have_key_events, a11y_no_noninteractive_element_interactions -->
<dialog
	bind:this={dlg}
	class="oidc hue-{SECTION_LOOK.password.hue}"
	aria-labelledby="oidc-h"
	onclose={() => (open = false)}
	onclick={(e) => e.target === dlg && (open = false)}
>
	<div class="card">
		<header>
			<span class="tile" aria-hidden="true"><Icon name="lock" size={16} /></span>
			<h2 id="oidc-h" tabindex="-1">{t('oidc.title')}</h2>
			<button type="button" class="x" aria-label={t('accounts.close')} onclick={() => (open = false)}><Icon name="x" size={16} /></button>
		</header>

		<ol>
			<li>
				<b>{t('oidc.step_app')}</b>
				<span>{t('oidc.step_app_sub')}</span>
				<div class="copybox one">
					<code>{redirect}</code>
					<button type="button" class="cp" class:ok={copied === 'r'} onclick={() => copy('r', redirect)}>
						<Icon name={copied === 'r' ? 'check' : 'copy'} />{copied === 'r' ? t('app.copied') : t('app.copy')}
					</button>
				</div>
			</li>
			<li>
				<b>{t('oidc.step_env')}</b>
				<span>{t('oidc.step_env_sub')}</span>
				<div class="copybox">
					<pre>{env}</pre>
					<button type="button" class="cp" class:ok={copied === 'e'} onclick={() => copy('e', env)}>
						<Icon name={copied === 'e' ? 'check' : 'copy'} />{copied === 'e' ? t('app.copied') : t('app.copy')}
					</button>
				</div>
			</li>
			<li>
				<b>{t('oidc.step_restart')}</b>
				<span>{t('oidc.step_restart_sub')}</span>
				<div class="copybox one">
					<code>{RESTART}</code>
					<button type="button" class="cp" class:ok={copied === 'c'} onclick={() => copy('c', RESTART)}>
						<Icon name={copied === 'c' ? 'check' : 'copy'} />{copied === 'c' ? t('app.copied') : t('app.copy')}
					</button>
				</div>
			</li>
		</ol>

		<p class="warn" role="note"><Icon name="alert" />{t('oidc.warn')}</p>

		<footer>
			<a class="btn ghost" href={GUIDE_URL} target="_blank" rel="noopener noreferrer">{t('oidc.full_guide')}</a>
			<button type="button" class="btn primary" onclick={() => (open = false)}>{t('accounts.done')}</button>
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
		border: 1px solid var(--border);
		border-radius: 14px;
		padding: 18px 20px 16px;
	}
	header {
		display: flex;
		align-items: center;
		gap: 10px;
		margin-bottom: 16px;
	}
	h2 {
		margin: 0;
		font-size: 16px;
		font-weight: 600;
		letter-spacing: -0.01em;
		outline: none;
	}
	.x {
		margin-left: auto;
		margin-right: -6px;
		display: grid;
		place-items: center;
		width: 36px;
		height: 36px;
		border: none;
		border-radius: 8px;
		background: none;
		color: var(--t2);
		cursor: pointer;
	}
	.x:hover {
		background: var(--surface-3);
		color: var(--text);
	}
	.x:focus-visible,
	.cp:focus-visible {
		outline: 2px solid var(--hue);
		outline-offset: 2px;
	}
	ol {
		margin: 0;
		padding: 0;
		list-style: none;
		counter-reset: step;
		display: grid;
		grid-template-columns: minmax(0, 1fr);
		gap: 16px;
	}
	li {
		counter-increment: step;
		position: relative;
		padding-left: 34px;
		font-size: 13px;
		line-height: 1.5;
		color: var(--t2);
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
		font-weight: 600;
		font-variant-numeric: tabular-nums;
		color: var(--hue);
		background: color-mix(in srgb, var(--hue) var(--soft), transparent);
	}
	li b {
		display: block;
		color: var(--text);
		font-weight: 600;
	}
	code,
	pre {
		font-family: var(--mono);
		font-size: 12px;
	}
	.copybox {
		display: flex;
		align-items: flex-start;
		gap: 8px;
		margin-top: 8px;
		padding: 8px 8px 8px 12px;
		border: 1px solid var(--border-soft);
		border-radius: 9px;
		background: var(--bg);
	}
	.copybox.one {
		align-items: center;
	}
	.copybox code {
		flex: 1;
		min-width: 0;
		overflow-wrap: anywhere;
		color: var(--text);
	}
	.copybox pre {
		flex: 1;
		min-width: 0;
		margin: 0;
		white-space: pre-wrap;
		overflow-wrap: anywhere;
		line-height: 1.6;
		color: var(--text);
	}
	.cp {
		display: inline-flex;
		align-items: center;
		gap: 5px;
		flex: none;
		font: inherit;
		font-size: 12.5px;
		font-weight: 500;
		padding: 3px 8px;
		border: 0;
		border-radius: 6px;
		background: none;
		color: var(--t2);
		cursor: pointer;
	}
	.cp:hover {
		color: var(--text);
		background: var(--surface-3);
	}
	.cp.ok {
		color: var(--green);
	}
	.warn {
		display: flex;
		align-items: flex-start;
		gap: 8px;
		margin: 18px 0 0;
		padding: 9px 12px;
		border-radius: 9px;
		font-size: 12.5px;
		line-height: 1.45;
		color: var(--amber);
		background: var(--amber-soft);
	}
	.warn :global(svg) {
		margin-top: 2px;
	}
	/* Phone: code gets the full width, Copy sits under it. */
	@media (max-width: 480px) {
		.card {
			padding: 16px;
		}
		li {
			padding-left: 30px;
		}
		.copybox {
			flex-direction: column;
			align-items: stretch;
		}
		.copybox.one {
			align-items: stretch;
		}
		.cp {
			align-self: flex-end;
		}
	}
	footer {
		display: flex;
		justify-content: flex-end;
		gap: 8px;
		margin-top: 16px;
	}
	/* footer beats the page's shared .btn rule, which has the same weight and loads later. */
	footer .btn.ghost {
		background: none;
		border-color: transparent;
		color: var(--t2);
	}
	footer .btn.ghost:hover {
		color: var(--text);
		border-color: var(--border);
	}
	footer .btn.primary {
		background: var(--accent-strong);
		border-color: var(--accent-strong);
		color: #fff;
	}
	footer .btn.primary:hover {
		background: color-mix(in srgb, var(--accent-strong) 85%, #fff);
	}
</style>

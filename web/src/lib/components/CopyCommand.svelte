<script lang="ts">
	import { onDestroy } from 'svelte';
	import { t } from '$lib/i18n/index.svelte';

	let { command }: { command: string } = $props();
	let copied = $state(false);
	let timer: ReturnType<typeof setTimeout> | undefined;

	function legacyCopy(text: string): boolean {
		const ta = document.createElement('textarea');
		ta.value = text;
		ta.style.position = 'fixed';
		ta.style.opacity = '0';
		document.body.appendChild(ta);
		ta.select();
		try {
			return document.execCommand('copy');
		} catch {
			return false;
		} finally {
			ta.remove();
		}
	}

	async function copy(): Promise<void> {
		let ok = false;
		try {
			await navigator.clipboard.writeText(command);
			ok = true;
		} catch {
			ok = legacyCopy(command);
		}
		if (!ok) return;
		copied = true;
		clearTimeout(timer);
		timer = setTimeout(() => (copied = false), 2000);
	}

	onDestroy(() => clearTimeout(timer));
</script>

<span class="cmd">
	<code>{command}</code>
	<button type="button" class:ok={copied} onclick={copy}>{copied ? t('app.copied') : t('app.copy')}</button>
</span>

<style>
	.cmd {
		display: flex;
		align-items: stretch;
		border: 1px solid var(--border);
		border-radius: 6px;
		background: var(--bg);
		overflow: hidden;
		max-width: 100%;
	}
	code {
		flex: 1;
		min-width: 0;
		padding: 7px 9px;
		font: 12px/1.45 var(--mono);
		color: var(--text);
		white-space: pre-wrap;
		word-break: break-word;
	}
	button {
		flex: none;
		border: 0;
		border-left: 1px solid var(--border);
		background: var(--surface);
		color: var(--t2);
		font: inherit;
		font-size: 12px;
		padding: 0 10px;
		min-width: 64px;
		cursor: pointer;
	}
	button:hover {
		color: var(--text);
		background: var(--surface-3);
	}
	button:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: -2px;
	}
	button.ok {
		color: var(--green);
	}
</style>

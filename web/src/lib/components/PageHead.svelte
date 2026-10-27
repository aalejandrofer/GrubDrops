<script lang="ts">
	import type { Snippet } from 'svelte';
	import { clock } from '$lib/format';
	import { t } from '$lib/i18n/index.svelte';
	import { getLive, getNet } from '$lib/live-context';
	import { prefs } from '$lib/prefs.svelte';
	import { loginFlow } from '$lib/login-flow.svelte';
	import { headerTone, problemChip, statusText } from '$lib/status';
	import CopyCommand from './CopyCommand.svelte';
	import Icon from './Icon.svelte';

	let {
		title,
		stamp,
		lead,
		status,
		action,
		children
	}: { title: string; stamp?: string; lead?: string; status?: string; action?: Snippet; children?: Snippet } = $props();
	const live = getLive();
	const net = getNet();
	const down = $derived(live.unreachable || net.down);
	const retryIn = $derived(live.unreachable ? live.retryIn : net.retryIn);

	const tone = $derived(headerTone(live.overview, down));
	const text = $derived(status && !down ? status : statusText(live.overview, down));
	const chip = $derived(problemChip(live.overview, loginFlow.id));
	const last = $derived(
		live.updatedAt === null
			? ''
			: t('status.last_updated', { time: clock(new Date(live.updatedAt).toISOString(), prefs.zone, prefs.fmt) })
	);
</script>

<div class="head">
	<div class="head-l">
		<h1>{title}</h1>
		{#if text}<span class="now {tone}"><span class="dot"></span>{text}</span>{/if}
		{#if lead && !down}<span class="stamp">{lead}</span>{/if}
		{#if down}
			<span class="alert red" role="status"
				><Icon name="alert" />{t('status.unreachable', { s: retryIn })}</span
			>
		{:else if chip}
			<a href={chip.href} class="alert"><Icon name="alert" />{chip.text}</a>
		{/if}
	</div>
	{#if down}
		{#if last}<span class="stamp last">{last}</span>{/if}
	{:else if action}
		{@render action()}
	{:else if stamp}
		<span class="stamp">{stamp}</span>
	{/if}
</div>

{#if down}
	<p class="help">
		<Icon name="terminal" />
		{t('status.check_container')}
		<CopyCommand command="docker compose ps" />
	</p>
{/if}

<div class="body" class:stale={down} inert={down}>
	{@render children?.()}
</div>

<style>
	.head {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 12px;
		margin-bottom: 24px;
		flex-wrap: wrap;
	}
	.head-l {
		display: flex;
		align-items: center;
		gap: 8px 16px;
		flex-wrap: wrap;
		min-width: 0;
	}
	h1 {
		font-size: 18px;
		font-weight: 600;
		margin: 0;
		letter-spacing: -0.01em;
	}
	.now {
		display: inline-flex;
		align-items: center;
		gap: 8px;
		font-size: 13px;
		font-weight: 500;
		color: var(--green);
	}
	.dot {
		width: 8px;
		height: 8px;
		border-radius: 50%;
		background: currentColor;
		flex: none;
	}
	.now.amber {
		color: var(--amber);
	}
	.now.red {
		color: var(--red);
	}
	.now.grey {
		color: var(--t2);
	}
	.now.grey .dot {
		background: var(--t3);
	}
	.alert {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		font-size: 12px;
		font-weight: 500;
		color: var(--amber);
		background: var(--amber-soft);
		border: 1px solid color-mix(in srgb, var(--amber) 30%, transparent);
		padding: 2px 8px;
		border-radius: 10px;
		white-space: nowrap;
		text-decoration: none;
	}
	a.alert:hover {
		border-color: var(--amber);
	}
	a.alert:focus-visible {
		outline: 2px solid var(--amber);
		outline-offset: 2px;
	}
	.alert.red {
		color: var(--red);
		background: var(--red-soft);
		border-color: color-mix(in srgb, var(--red) 35%, transparent);
	}
	.stamp {
		color: var(--t3);
		font-size: 12px;
		font-family: var(--mono);
	}
	.stamp.last {
		color: var(--t2);
	}
	.help {
		display: flex;
		align-items: center;
		gap: 8px 10px;
		flex-wrap: wrap;
		margin: -8px 0 20px;
		font-size: 13px;
		color: var(--text);
	}
	.help :global(.i) {
		color: var(--t2);
	}
	.help :global(.cmd) {
		display: inline-flex;
	}
	.help :global(code) {
		padding: 3px 9px;
		white-space: nowrap;
	}
	.help :global(button) {
		min-width: 56px;
		padding: 0 8px;
	}
	.stale {
		opacity: 0.55;
		filter: saturate(0.4);
		user-select: none;
	}
</style>

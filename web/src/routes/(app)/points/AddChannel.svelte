<script lang="ts">
	import { tick } from 'svelte';
	import type { ForceChannel, Platform } from '$lib/api/types';
	import Hint, { type HintExample } from '$lib/components/Hint.svelte';
	import { slugRange } from '$lib/hint';
	import { t } from '$lib/i18n/index.svelte';
	import { addChannel } from '$lib/points';

	/** Paste a channel link of the open tab; it lands at the bottom with no accounts ticked. */
	let {
		tab,
		list,
		label,
		onadd
	}: { tab: Platform; list: ForceChannel[]; label: Record<Platform, string>; onadd: (next: ForceChannel[], key: string) => void } =
		$props();

	let url = $state('');
	let err = $state('');
	let input = $state<HTMLInputElement>();

	const EXAMPLES: Record<Platform, HintExample[]> = {
		twitch: [{ platform: 'twitch', label: 'twitch.tv/streamer', value: 'https://www.twitch.tv/streamer' }],
		kick: [{ platform: 'kick', label: 'kick.com/streamer', value: 'https://kick.com/streamer' }]
	};

	/** Fills the box with the link and selects "streamer", so typing the channel name replaces it. */
	async function pick(link: string): Promise<void> {
		url = link;
		err = '';
		await tick();
		input?.focus();
		input?.setSelectionRange(...slugRange(link));
	}

	function submit(e: SubmitEvent): void {
		e.preventDefault();
		const r = addChannel(list, url, tab);
		if ('err' in r) {
			err =
				r.err === 'bad'
					? t('force.url_bad')
					: r.err === 'dupe'
						? t('force.dupe')
						: t('points.other_tab', { platform: label[r.platform ?? tab] });
			return;
		}
		url = '';
		onadd(r.list, r.key);
	}
</script>

<form class="add" onsubmit={submit}>
	<label>
		<span class="rh">{t('points.add_label', { platform: label[tab] })}</span>
		<span class="row">
			<input
				type="url"
				placeholder={tab === 'twitch' ? 'https://www.twitch.tv/keshaeuw' : 'https://kick.com/xqc'}
				bind:value={url}
				bind:this={input}
				oninput={() => (err = '')}
			/>
			<button type="submit" class="btn primary" disabled={!url.trim()}>{t('force.add_btn')}</button>
		</span>
	</label>
	{#if err}<p class="bad" role="alert">{err}</p>{:else}<Hint examples={EXAMPLES[tab]} onpick={pick} />{/if}
</form>

<style>
	.add {
		margin-top: 16px;
		max-width: 520px;
	}
	label {
		display: grid;
	}
	.rh {
		margin: 0 0 8px;
		font-size: 11px;
		font-weight: 600;
		text-transform: uppercase;
		letter-spacing: 0.06em;
		color: var(--t3);
	}
	.row {
		display: flex;
		gap: 8px;
	}
	input::selection {
		background: color-mix(in srgb, var(--accent) 35%, transparent);
		color: var(--text);
	}
	input {
		flex: 1;
		min-width: 0;
		font: inherit;
		font-size: 13px;
		height: 30px;
		padding: 0 10px;
		border-radius: 6px;
		border: 1px solid var(--border);
		background: var(--bg);
		color: var(--text);
	}
	.bad {
		margin: 6px 0 0;
		font-size: 12px;
		color: var(--red);
	}
</style>

<script lang="ts">
	import type { PointsMode } from '$lib/api/types';
	import { msToNextMinute } from '$lib/format';
	import Home from './_Home.svelte';
	import Points from './_Points.svelte';
	import { pair, pairOrder, six, sixOrder, startedAt, type Acc, type Variant } from './_data';

	let { variant }: { variant: Variant } = $props();

	const MIN = 60_000;
	const loadedAt = Date.now();

	// One tick a minute, on the minute, as the real timer would. skip moves
	// the clock forward so the 1h and 1d formats can be seen without waiting.
	let real = $state(loadedAt);
	let skip = $state(0);
	const now = $derived(real + skip * MIN);
	$effect(() => {
		let id: ReturnType<typeof setTimeout>;
		const tick = (): void => {
			real = Date.now();
			id = setTimeout(tick, msToNextMinute(real));
		};
		id = setTimeout(tick, msToNextMinute(Date.now()));
		return () => clearTimeout(id);
	});

	/** D only: three watching, or two, to show both header forms. */
	let three = $state(true);
	let modes = $state<Record<string, PointsMode>>({});

	const dash = $derived(variant.id === 'd');
	const accs = $derived(
		(dash ? six : pair).map((x): Acc => {
			const idle = dash && !three && x.a.id === 'acc_3';
			return {
				...x,
				a: { ...x.a, points_mode: modes[x.a.id] ?? x.a.points_mode },
				channel: idle ? null : x.channel,
				line: idle ? 'Idle' : x.line
			};
		})
	);
	const start = (x: Acc) => startedAt(loadedAt, x.onFor);
	const readAt = (x: Acc) => new Date(loadedAt - x.readAgo * MIN).toISOString();
	const onmode = (id: string, m: PointsMode) => (modes[id] = m);
</script>

<div class="ctl">
	<span class="lbl">Clock</span>
	<button type="button" class="btn" onclick={() => (skip += 15)}>+15 min</button>
	<button type="button" class="btn" onclick={() => (skip += 60 * 22)}>+22 h</button>
	<button type="button" class="btn" disabled={skip === 0} onclick={() => (skip = 0)}>Reset</button>
	{#if dash}
		<span class="lbl gap">Watching</span>
		<div class="seg" role="group" aria-label="Accounts watching">
			<button type="button" aria-pressed={three} onclick={() => (three = true)}>3</button>
			<button type="button" aria-pressed={!three} onclick={() => (three = false)}>2</button>
		</div>
	{/if}
	<span class="hintc">Ticks once a minute, on the minute.</span>
</div>

{#if variant.home}
	<p class="where">Home</p>
	<div class="frame">
		<Home accs={pair} {now} {start} timer />
	</div>
{/if}

<p class="where">Points</p>
<div class="frame">
	<Points {variant} {accs} order={dash ? sixOrder : pairOrder} {now} {start} {readAt} {onmode} />
</div>

<style>
	.ctl {
		display: flex;
		align-items: center;
		flex-wrap: wrap;
		gap: 8px;
		margin: 0 0 16px;
	}
	.lbl {
		font-size: 12px;
		color: var(--t3);
	}
	.lbl.gap {
		margin-left: 8px;
	}
	.hintc {
		font-size: 12px;
		color: var(--dim);
	}
	.btn {
		font: inherit;
		font-size: 12px;
		font-weight: 500;
		height: 28px;
		padding: 0 10px;
		border-radius: 6px;
		border: 1px solid var(--border);
		background: var(--surface-2);
		color: var(--text);
		cursor: pointer;
	}
	.btn:hover:not([disabled]) {
		border-color: var(--t3);
	}
	.btn[disabled] {
		opacity: 0.45;
		cursor: default;
	}
	.seg {
		display: inline-flex;
		border: 1px solid var(--border);
		border-radius: 6px;
		overflow: hidden;
	}
	.seg button {
		font: inherit;
		font-size: 12px;
		height: 26px;
		padding: 0 12px;
		border: 0;
		background: var(--surface);
		color: var(--t2);
		cursor: pointer;
	}
	.seg button + button {
		border-left: 1px solid var(--border);
	}
	.seg button[aria-pressed='true'] {
		background: var(--accent-soft);
		color: var(--accent);
	}
	.btn:focus-visible,
	.seg button:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 2px;
	}
	.where {
		margin: 20px 0 6px;
		font-size: 12px;
		color: var(--t3);
	}
	.frame {
		padding: 24px;
		border: 1px solid var(--border);
		border-radius: 12px;
		background: var(--bg);
		color: var(--text);
	}
	@media (max-width: 720px) {
		.frame {
			padding: 14px;
		}
	}
</style>

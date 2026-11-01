<script lang="ts">
	import Accounts from './_Accounts.svelte';
	import Channels from './_Channels.svelte';
	import Events from './_Events.svelte';
	import Experimental from './_Experimental.svelte';
	import General from './_General.svelte';
	import Head from './_Head.svelte';
	import Health from './_Health.svelte';
	import Ico from './_Ico.svelte';
	import Network from './_Network.svelte';
	import Priority from './_Priority.svelte';
	import Security from './_Security.svelte';
	import Sleep from './_Sleep.svelte';
	import Top from './_Top.svelte';
	import Updates from './_Updates.svelte';
	import type { Hue, IconName } from './_data';
	import { st } from './_state.svelte';

	/** Today's eight sections, same order, each with its own hue and icon.
	 *  Red, amber and green stay for status (chips, dots), so no section uses them except Health. */
	type Id = 'general' | 'accounts' | 'engine' | 'experimental' | 'notifications' | 'health' | 'updates' | 'password';
	const SECS: { id: Id; label: string; icon: IconName; hue: Hue }[] = [
		{ id: 'general', label: 'General', icon: 'sliders', hue: 'blue' },
		{ id: 'accounts', label: 'Accounts', icon: 'users', hue: 'violet' },
		{ id: 'engine', label: 'Engine', icon: 'cpu', hue: 'orange' },
		{ id: 'experimental', label: 'Experimental', icon: 'flask', hue: 'pink' },
		{ id: 'notifications', label: 'Notifications', icon: 'bell', hue: 'teal' },
		{ id: 'health', label: 'Health', icon: 'pulse', hue: 'green' },
		{ id: 'updates', label: 'Updates', icon: 'download', hue: 'blue' },
		{ id: 'password', label: 'Authentication', icon: 'lock', hue: 'indigo' }
	];
	const by = Object.fromEntries(SECS.map((s) => [s.id, s])) as Record<Id, (typeof SECS)[number]>;

	/** Small state dot on the nav link: amber needs you, red is failing. */
	const DOT_TEXT = { warn: 'needs attention', bad: 'failing' } as const;
	const DOT: Partial<Record<Id, 'warn' | 'bad'>> = $derived({
		accounts: st.counts.bad ? 'warn' : undefined,
		health: 'bad'
	});

	let active = $state<Id>('general');
	let hold = 0;
	function track(): void {
		if (Date.now() < hold) return;
		if (window.innerHeight + window.scrollY >= document.documentElement.scrollHeight - 2) {
			active = SECS[SECS.length - 1].id;
			return;
		}
		let cur: Id = 'general';
		for (const s of SECS) {
			const el = document.getElementById(`cs-${s.id}`);
			if (el && el.getBoundingClientRect().top <= 140) cur = s.id;
		}
		active = cur;
	}
	function jump(id: Id): void {
		active = id;
		hold = Date.now() + 800;
		document.getElementById(`cs-${id}`)?.scrollIntoView({ behavior: 'smooth' });
	}

	/** Phone: keep the active pill in view as the page scrolls. */
	$effect(() => {
		const el = document.getElementById(`cn-${active}`);
		const row = el?.parentElement;
		if (!el || !row || row.scrollWidth <= row.clientWidth) return;
		row.scrollTo({ left: el.offsetLeft - row.clientWidth / 2 + el.offsetWidth / 2, behavior: 'smooth' });
	});
</script>

<svelte:window onscroll={track} />

<div class="sx colorful">
	<Top />
	<div class="layout">
		<nav class="subnav" aria-label="Settings sections">
			{#each SECS as s (s.id)}
				<a
					id="cn-{s.id}"
					class="navlink hue-{s.hue}"
					class:on={active === s.id}
					href="#cs-{s.id}"
					aria-current={active === s.id ? 'true' : undefined}
					onclick={(e) => (e.preventDefault(), jump(s.id))}
				>
					<span class="tile sm"><Ico name={s.icon} size={13} /></span>
					<span class="nm">{s.label}</span>
					{#if DOT[s.id]}{@const d = DOT[s.id]!}<span class="dot {d}" aria-hidden="true"></span><span class="vh">, {DOT_TEXT[d]}</span>{/if}
				</a>
			{/each}
		</nav>

		<div class="secs">
			<section id="cs-general" class="set hue-blue">
				<Head label="General" icon="sliders" hue="blue" />
				<General />
				<Security part="backup" />
			</section>

			<section id="cs-accounts" class="set hue-violet">
				<Head label="Accounts" icon="users" hue="violet" blurb="Each account mines your games.">
					{#snippet chip()}{#if st.counts.bad}<span class="chip warn">{st.counts.bad} needs login</span>{/if}{/snippet}
				</Head>
				<Accounts />
			</section>

			<section id="cs-engine" class="set hue-orange">
				<Head label="Engine" icon="cpu" hue="orange" blurb="What gets mined, when, and how." />
				<Priority />
				<div class="sub-h"><span class="tile sm"><Ico name="moon" size={13} /></span>Sleep window</div>
				<Sleep />
				<div class="sub-h"><span class="tile sm"><Ico name="globe" size={13} /></span>Network</div>
				<Network />
			</section>

			<section id="cs-experimental" class="set hue-pink">
				<Head label="Experimental" icon="flask" hue="pink" blurb="May change or go away." />
				<Experimental />
			</section>

			<section id="cs-notifications" class="set hue-teal">
				<Head label="Notifications" icon="bell" hue="teal">
					{#snippet chip()}<span class="chip ok">Discord on</span>{/snippet}
				</Head>
				<Channels />
				<div class="sub-h"><span class="tile sm"><Ico name="bell" size={13} /></span>Notify me when</div>
				<Events />
			</section>

			<section id="cs-health" class="set hue-green">
				<Head label="Health" icon="pulse" hue="green" blurb="Miner activity, newest first.">
					{#snippet chip()}<span class="chip bad">Kick canary failing</span>{/snippet}
				</Head>
				<Health />
			</section>

			<section id="cs-updates" class="set hue-blue">
				<Head label="Updates" icon="download" hue="blue">
					{#snippet chip()}<span class="chip ok">Up to date</span>{/snippet}
				</Head>
				<Updates chip={false} />
			</section>

			<section id="cs-password" class="set hue-indigo">
				<Head label={by.password.label} icon="lock" hue="indigo" blurb="Single sign-on and admin password." />
				<Security part="password" />
			</section>
		</div>
	</div>
</div>

<style>
	.layout {
		display: grid;
		grid-template-columns: 196px minmax(0, 820px);
		gap: 40px;
		align-items: start;
	}
	.subnav {
		position: sticky;
		top: 16px;
		display: grid;
		gap: 2px;
	}
	.navlink {
		display: flex;
		align-items: center;
		gap: 10px;
		padding: 5px 8px;
		border-radius: 8px;
		color: var(--t2);
		font-size: 13.5px;
		font-weight: 500;
	}
	.navlink:hover {
		color: var(--text);
		background: var(--surface);
	}
	.navlink.on {
		color: var(--text);
		font-weight: 600;
		background: color-mix(in srgb, var(--hue) 16%, transparent);
	}
	.nm {
		flex: 1;
	}
	.dot {
		width: 7px;
		height: 7px;
		border-radius: 50%;
	}
	.dot.warn {
		background: var(--amber);
	}
	.dot.bad {
		background: var(--red);
	}
	section.set {
		scroll-margin-top: 40px;
		padding: 0 0 36px;
		margin-bottom: 36px;
		border-bottom: 1px solid var(--border-soft);
	}
	section.set:last-child {
		border-bottom: 0;
	}
	@media (max-width: 720px) {
		.layout {
			grid-template-columns: minmax(0, 1fr);
			gap: 16px;
		}
		.subnav {
			top: 0;
			z-index: 12;
			display: flex;
			gap: 6px;
			overflow-x: auto;
			scrollbar-width: none;
			margin: 0 -16px;
			padding: 10px 16px;
			background: color-mix(in srgb, var(--bg) 94%, transparent);
			border-bottom: 1px solid var(--border-soft);
		}
		.subnav::-webkit-scrollbar {
			display: none;
		}
		.navlink {
			flex: none;
			min-height: 36px;
			padding: 6px 12px 6px 7px;
			border: 1px solid var(--border-soft);
			border-radius: 999px;
		}
		.navlink.on {
			/* Same weight as idle pills so the row does not shift as you scroll. */
			font-weight: 500;
			border-color: color-mix(in srgb, var(--fill) 60%, transparent);
		}
		.navlink .tile {
			width: 22px;
			height: 22px;
			border-radius: 50%;
		}
		section.set {
			scroll-margin-top: 88px;
		}
	}
</style>

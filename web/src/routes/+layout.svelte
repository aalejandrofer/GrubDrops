<script lang="ts">
	import '../app.css';
	import { api, session } from '$lib/app';
	import { matchLang, setLang } from '$lib/i18n/index.svelte';
	import { applyPrefs } from '$lib/prefs.svelte';
	import favicon16 from '../../../assets/img/favicon-16.png';
	import favicon32 from '../../../assets/img/favicon-32.png';
	import touchIcon from '../../../assets/img/apple-touch-icon.png';

	let { children } = $props();

	// Language from the browser until settings load; theme and language from settings once authed.
	setLang(matchLang(typeof navigator === 'undefined' ? [] : navigator.languages));

	let applied = false;
	$effect(() => {
		if (!session.authed) {
			applied = false;
			return;
		}
		if (applied) return;
		applied = true;
		api
			.settings()
			.then(applyPrefs)
			.catch(() => {
				applied = false;
			});
	});
</script>

<svelte:head>
	<link rel="icon" type="image/png" sizes="32x32" href={favicon32} />
	<link rel="icon" type="image/png" sizes="16x16" href={favicon16} />
	<link rel="apple-touch-icon" href={touchIcon} />
</svelte:head>

{@render children()}

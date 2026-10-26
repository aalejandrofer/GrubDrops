<script lang="ts">
	import '../app.css';
	import { api, session } from '$lib/app';
	import { matchLang, setLang } from '$lib/i18n/index.svelte';
	import { applyPrefs } from '$lib/prefs.svelte';

	let { children } = $props();

	// Language from the browser until settings load; theme and language from settings once authed.
	setLang(matchLang(typeof navigator === 'undefined' ? [] : navigator.languages));

	let applied = false;
	$effect(() => {
		if (!session.authed || applied) return;
		applied = true;
		api
			.settings()
			.then(applyPrefs)
			.catch(() => {
				applied = false;
			});
	});
</script>

{@render children()}

<script lang="ts">
	import Accounts from './_Accounts.svelte';
	import Channels from './_Channels.svelte';
	import Events from './_Events.svelte';
	import Experimental from './_Experimental.svelte';
	import General from './_General.svelte';
	import Health from './_Health.svelte';
	import Network from './_Network.svelte';
	import Priority from './_Priority.svelte';
	import Security from './_Security.svelte';
	import Sleep from './_Sleep.svelte';
	import Updates from './_Updates.svelte';
	import type { SecId } from './_data';

	let { id }: { id: SecId } = $props();
</script>

{#if id === 'accounts'}<Accounts />
{:else if id === 'priority'}<Priority />
{:else if id === 'sleep'}<Sleep />
{:else if id === 'network'}<Network />
{:else if id === 'experimental'}<Experimental />
{:else if id === 'channels'}<Channels />
{:else if id === 'events'}<Events />
{:else if id === 'general'}<General /><Security part="backup" />
{:else if id === 'health'}<Health />
{:else if id === 'updates'}<Updates />
{:else}<Security part="password" />{/if}

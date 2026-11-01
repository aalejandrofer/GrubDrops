<script lang="ts">
	/**
	 * Save countdown toast, fixed top right under the top bar. On phones it spans
	 * the width under the top bar; a page with its own sticky row under the bar
	 * sets --toast-top-phone to sit below it. It has no controls, so clicks
	 * pass through to whatever it covers.
	 */
	let { text }: { text: string } = $props();
</script>

<p class="toast" role="status">{text}</p>

<style>
	.toast {
		position: fixed;
		top: var(--toast-top, 64px);
		right: 24px;
		z-index: 50;
		pointer-events: none;
		margin: 0;
		padding: 10px 16px;
		max-width: min(360px, calc(100vw - 48px));
		border: 1px solid color-mix(in srgb, var(--amber) 50%, var(--border));
		border-radius: 8px;
		background: var(--surface-2);
		color: var(--text);
		font-size: 13px;
		font-variant-numeric: tabular-nums;
		box-shadow: 0 4px 16px rgb(0 0 0 / 0.2);
	}
	@media (max-width: 720px) {
		.toast {
			top: var(--toast-top-phone, 60px);
			left: 16px;
			right: 16px;
			max-width: none;
		}
	}
</style>

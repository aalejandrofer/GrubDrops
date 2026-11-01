// "Expand advanced settings": a per-browser pref that keeps every Advanced section in Settings open.

export const EXPAND_ADV_KEY = 'grub.settings.expand_advanced';

/** Live value of the pref; Settings reads it, General flips it. */
export const advanced: { expand: boolean } = $state({ expand: typeof window !== 'undefined' && readExpandAdv() });

export function readExpandAdv(): boolean {
	try {
		return localStorage.getItem(EXPAND_ADV_KEY) === '1';
	} catch {
		return false;
	}
}

export function setExpandAdv(on: boolean): void {
	advanced.expand = on;
	try {
		if (on) localStorage.setItem(EXPAND_ADV_KEY, '1');
		else localStorage.removeItem(EXPAND_ADV_KEY);
	} catch {
		/* storage unavailable: the choice lasts for this visit */
	}
}

/** The parts of a DOM node the helpers below read, so tests can pass plain objects. */
export type AdvNode = {
	matches?: (sel: string) => boolean;
	querySelectorAll?: (sel: string) => Iterable<{ open: boolean }>;
	open?: boolean;
};

const ADV = 'details.adv';

/** Every Advanced section in or at `node`. */
export function advancedIn(node: AdvNode): { open: boolean }[] {
	const out: { open: boolean }[] = [];
	if (node.matches?.(ADV)) out.push(node as { open: boolean });
	if (node.querySelectorAll) out.push(...node.querySelectorAll(ADV));
	return out;
}

/**
 * Whether an Advanced section shows open. With the pref on it is open on every render;
 * otherwise it is open only while the user keeps it open.
 */
export function advOpen(pref: boolean, userOpen: boolean): boolean {
	return pref || userOpen;
}

/**
 * Keeps the Advanced sections under `root` in line with the pref. Flipping the pref opens or
 * closes them all; with it on, a section that renders later starts open. A section the user
 * collapses stays collapsed until it renders again.
 */
export function syncAdvanced(root: HTMLElement, pref: boolean): () => void {
	for (const d of advancedIn(root)) d.open = advOpen(pref, false);
	if (!pref) return () => {};
	const mo = new MutationObserver((records) => {
		for (const r of records) for (const n of r.addedNodes) for (const d of advancedIn(n as AdvNode)) d.open = advOpen(pref, d.open);
	});
	mo.observe(root, { childList: true, subtree: true });
	return () => mo.disconnect();
}

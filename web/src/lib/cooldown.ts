/**
 * A refresh button's label as a locale key: "Refreshing" while busy, else the
 * idle key. Done keeps the idle text too: the button's green says it landed.
 */
export function refreshLabel(busy: boolean, done: boolean, idleKey: string): string {
	return busy ? 'points.refreshing' : idleKey;
}

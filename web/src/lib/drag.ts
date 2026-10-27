// Drag-to-reorder for a list. One action serves every reorderable list: the
// listeners live on window (no pointer capture), so the keyed list can move
// DOM nodes while a drag runs without ending it.

export const DRAG_THRESHOLD = 4;
export const DRAGGING_CLASS = 'grub-dragging';

const INTERACTIVE = 'button, a, input, select, textarea, label, [role="menu"], [contenteditable], [data-campaign]';

type Closest = { closest(sel: string): unknown };

/** True when the pointerdown landed on something that handles its own clicks. */
export function isInteractive(target: Closest | null): boolean {
	return !target || target.closest(INTERACTIVE) !== null;
}

/** True once the pointer moved far enough from its start to count as a drag. */
export function pastThreshold(dx: number, dy: number, px = DRAG_THRESHOLD): boolean {
	return Math.hypot(dx, dy) >= px;
}

/**
 * Final index of the dragged row: the number of other rows whose midpoint sits
 * above the pointer. Stable while the row follows the pointer, so no flicker.
 */
export function targetIndex(y: number, rects: { top: number; bottom: number }[], cur: number): number {
	let n = 0;
	rects.forEach((r, i) => {
		if (i !== cur && (r.top + r.bottom) / 2 < y) n++;
	});
	return n;
}

export interface ReorderOptions {
	/** Rows of the list in order; defaults to the list's children. */
	items?: (list: HTMLElement) => HTMLElement[];
	/** False blocks a new drag, e.g. while a save runs. */
	enabled?: () => boolean;
	onStart: (from: number) => void;
	onMove: (to: number) => void;
	/** Pointer released. `to` may equal `from`. */
	onEnd: (from: number, to: number) => void;
	/** Escape, pointercancel or destroy. */
	onCancel: () => void;
}

export function reorder(list: HTMLElement, initial: ReorderOptions): { update(o: ReorderOptions): void; destroy(): void } {
	let opts = initial;
	let from = -1;
	let to = -1;
	let startX = 0;
	let startY = 0;
	let pointerId = -1;
	let active = false;
	let pending = false;

	const items = () => (opts.items ? opts.items(list) : (Array.from(list.children) as HTMLElement[]));

	function block(e: Event): void {
		e.preventDefault();
	}

	function stop(): void {
		window.removeEventListener('pointermove', onMove);
		window.removeEventListener('pointerup', onUp);
		window.removeEventListener('pointercancel', onCancel);
		window.removeEventListener('keydown', onKey);
		window.removeEventListener('selectstart', block, true);
		window.removeEventListener('dragstart', block, true);
		document.documentElement.classList.remove(DRAGGING_CLASS);
		pending = false;
		active = false;
	}

	function onDown(e: PointerEvent): void {
		if (pending || active || e.button !== 0 || !e.isPrimary) return;
		if (opts.enabled && !opts.enabled()) return;
		const target = e.target as Element | null;
		if (isInteractive(target)) return;
		const rows = items();
		const row = rows.find((r) => r.contains(target));
		if (!row) return;
		from = to = rows.indexOf(row);
		startX = e.clientX;
		startY = e.clientY;
		pointerId = e.pointerId;
		pending = true;
		window.addEventListener('pointermove', onMove);
		window.addEventListener('pointerup', onUp);
		window.addEventListener('pointercancel', onCancel);
		window.addEventListener('keydown', onKey);
		// A press on text or an image must not start a selection or a native image drag.
		window.addEventListener('selectstart', block, true);
		window.addEventListener('dragstart', block, true);
	}

	function onMove(e: PointerEvent): void {
		if (e.pointerId !== pointerId) return;
		if (!active) {
			if (!pastThreshold(e.clientX - startX, e.clientY - startY)) return;
			active = true;
			document.documentElement.classList.add(DRAGGING_CLASS);
			window.getSelection()?.removeAllRanges();
			opts.onStart(from);
		}
		e.preventDefault();
		const next = targetIndex(
			e.clientY,
			items().map((r) => r.getBoundingClientRect()),
			to
		);
		if (next !== to) {
			to = next;
			opts.onMove(to);
		}
	}

	function onUp(e: PointerEvent): void {
		if (e.pointerId !== pointerId) return;
		const was = active;
		const f = from;
		const t = to;
		stop();
		if (was) opts.onEnd(f, t);
	}

	function onCancel(e?: Event): void {
		if (e instanceof PointerEvent && e.pointerId !== pointerId) return;
		const was = active;
		stop();
		if (was) opts.onCancel();
	}

	function onKey(e: KeyboardEvent): void {
		if (e.key === 'Escape') onCancel();
	}

	list.addEventListener('pointerdown', onDown);
	return {
		update(o) {
			opts = o;
		},
		destroy() {
			list.removeEventListener('pointerdown', onDown);
			onCancel();
		}
	};
}

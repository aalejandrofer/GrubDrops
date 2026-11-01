import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { AUTOSAVE_MS, Autosave, SAVED_HOLD_MS, typed } from './autosave';

beforeEach(() => {
	vi.useFakeTimers();
});
afterEach(() => {
	vi.useRealTimers();
});

describe('Autosave', () => {
	it('saves 10s after the last edit', async () => {
		const a = new Autosave();
		const run = vi.fn(async () => true);
		a.edit('proxy', run);
		expect(a.phase).toBe('wait');
		expect(a.secondsLeft(Date.now())).toBe(10);
		await vi.advanceTimersByTimeAsync(AUTOSAVE_MS - 1);
		expect(run).not.toHaveBeenCalled();
		await vi.advanceTimersByTimeAsync(1);
		expect(run).toHaveBeenCalledTimes(1);
		expect(a.has()).toBe(false);
		expect(a.phase).toBe('saved');
		await vi.advanceTimersByTimeAsync(SAVED_HOLD_MS);
		expect(a.phase).toBe('idle');
	});

	it('restarts the countdown on every edit, for any field', async () => {
		const a = new Autosave();
		const proxy = vi.fn(async () => true);
		const sleep = vi.fn(async () => true);
		a.edit('proxy', proxy);
		await vi.advanceTimersByTimeAsync(8000);
		a.edit('sleep', sleep);
		await vi.advanceTimersByTimeAsync(8000);
		expect(proxy).not.toHaveBeenCalled();
		expect(a.secondsLeft(Date.now())).toBe(2);
		await vi.advanceTimersByTimeAsync(2000);
		expect(proxy).toHaveBeenCalledTimes(1);
		expect(sleep).toHaveBeenCalledTimes(1);
	});

	it('does not save an invalid field and keeps it pending', async () => {
		const a = new Autosave();
		let value = 'bad';
		const saved: string[] = [];
		// Like a field's save: validates first and shows its error instead of saving.
		const run = vi.fn(async () => {
			if (value === 'bad') return false;
			saved.push(value);
			return true;
		});
		a.edit('sleep', run, () => value !== 'bad');
		expect(a.invalid()).toEqual(['sleep']);
		await vi.advanceTimersByTimeAsync(AUTOSAVE_MS);
		expect(run).toHaveBeenCalledTimes(1);
		expect(saved).toEqual([]);
		expect(a.has('sleep')).toBe(true);
		expect(a.phase).toBe('idle');
		expect(a.dueAt).toBe(0);
		// Fixed and edited again: it saves on the next countdown.
		value = '23:00';
		a.edit('sleep', run, () => value !== 'bad');
		await vi.advanceTimersByTimeAsync(AUTOSAVE_MS);
		expect(saved).toEqual(['23:00']);
		expect(a.has()).toBe(false);
	});

	it('keeps the edit when the save fails', async () => {
		const a = new Autosave();
		a.edit('webhook', async () => {
			throw new Error('down');
		});
		await vi.advanceTimersByTimeAsync(AUTOSAVE_MS);
		expect(a.has('webhook')).toBe(true);
		expect(a.phase).toBe('idle');
	});

	it('flushes at once on leave, all fields in one go', async () => {
		const a = new Autosave();
		const order: string[] = [];
		a.edit('proxy', () => (order.push('proxy'), Promise.resolve(true)));
		a.edit('avatar', () => (order.push('avatar'), Promise.resolve(true)));
		const p = a.flush(true);
		// Every save has started before the promise settles, so an unloading page loses none.
		expect(order).toEqual(['proxy', 'avatar']);
		expect(await p).toBe(true);
		expect(a.has()).toBe(false);
		await vi.advanceTimersByTimeAsync(AUTOSAVE_MS);
		expect(order).toHaveLength(2);
	});

	it('saves one field now and leaves the rest counting', async () => {
		const a = new Autosave();
		const proxy = vi.fn(async () => true);
		const avatar = vi.fn(async () => true);
		a.edit('proxy', proxy);
		a.edit('avatar', avatar);
		expect(await a.now('proxy', proxy)).toBe(true);
		expect(a.has('proxy')).toBe(false);
		expect(a.phase).toBe('wait');
		await vi.advanceTimersByTimeAsync(AUTOSAVE_MS);
		expect(proxy).toHaveBeenCalledTimes(1);
		expect(avatar).toHaveBeenCalledTimes(1);
	});

	it('stops the countdown when the last field is dropped', async () => {
		const a = new Autosave();
		const run = vi.fn(async () => true);
		a.edit('webhook', run);
		a.drop('webhook');
		expect(a.phase).toBe('idle');
		await vi.advanceTimersByTimeAsync(AUTOSAVE_MS);
		expect(run).not.toHaveBeenCalled();
	});

	it('an edit during a save keeps the new edit pending', async () => {
		const a = new Autosave();
		let finish!: (ok: boolean) => void;
		a.edit('proxy', () => new Promise<boolean>((r) => (finish = r)));
		await vi.advanceTimersByTimeAsync(AUTOSAVE_MS);
		expect(a.phase).toBe('saving');
		const next = vi.fn(async () => true);
		a.edit('proxy', next);
		finish(false);
		await vi.advanceTimersByTimeAsync(0);
		expect(a.phase).toBe('wait');
		await vi.advanceTimersByTimeAsync(AUTOSAVE_MS);
		expect(next).toHaveBeenCalledTimes(1);
	});
});

describe('typed', () => {
	it('starts a save for typing, pasting and deleting', () => {
		expect(typed({ isTrusted: true, inputType: 'insertText' })).toBe(true);
		expect(typed({ isTrusted: true, inputType: 'insertFromPaste' })).toBe(true);
		expect(typed({ isTrusted: true, inputType: 'deleteContentBackward' })).toBe(true);
	});
	it('ignores autofill and scripted input', () => {
		expect(typed({ isTrusted: true })).toBe(false);
		expect(typed({ isTrusted: true, inputType: '' })).toBe(false);
		expect(typed({ isTrusted: true, inputType: 'insertReplacementText' })).toBe(false);
		expect(typed({ isTrusted: false, inputType: 'insertText' })).toBe(false);
	});
});

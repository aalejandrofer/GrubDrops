import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterEach, describe, expect, it } from 'vitest';
import type { HealthEvent } from '$lib/api/types';
import { setLang } from '$lib/i18n/index.svelte';
import { MSG_KEYS, eventText, isProblem, kindLabel } from './health';

const ev = (o: Partial<HealthEvent>): HealthEvent => ({
	at: '2026-01-01T00:00:00Z',
	level: 'INFO',
	kind: 'info',
	account_id: '',
	account_name: '',
	msg: '',
	...o
});

afterEach(() => setLang('en'));

describe('eventText', () => {
	it('mapped msg is localised with fields', () => {
		setLang('es');
		const s = eventText(ev({ kind: 'discovery', msg: 'runner picked drop', fields: { game: 'Rust' } }));
		expect(s).toBe('Drop elegido para Rust');
	});
	it('missing field falls back to raw msg', () => {
		expect(eventText(ev({ msg: 'runner picked drop', fields: {} }))).toBe('runner picked drop');
	});
	it('unknown msg shows raw', () => {
		expect(eventText(ev({ msg: 'something new' }))).toBe('something new');
	});
	it('state and prev go through the runner state labels', () => {
		setLang('es');
		expect(eventText(ev({ msg: 'runner state change', fields: { prev: 'idle', state: 'needs_login' } }))).toContain('Inactiva');
		expect(eventText(ev({ msg: 'runner state change', fields: { prev: 'idle', state: 'weird' } }))).toContain('weird');
	});
	it('err appended once', () => {
		expect(eventText(ev({ msg: 'runner progress', fields: { err: 'boom' } }))).toBe('Watch progress · boom');
		expect(eventText(ev({ msg: 'runner claim failed', fields: { err: 'boom' } }))).toBe('Claim failed: boom');
		expect(eventText(ev({ msg: 'odd', fields: { err: 'boom' } }))).toBe('odd · boom');
	});
});

describe('kindLabel', () => {
	it('maps known kinds, passes unknown through', () => {
		expect(kindLabel('claim')).toBe('Claim');
		expect(kindLabel('heartbeat')).toBe('Heartbeat');
				expect(kindLabel('toString')).toBe('toString');
	});
});

describe('isProblem', () => {
	it('flags WARN, ERROR and auth, not info progress', () => {
		expect(isProblem(ev({ level: 'WARN' }))).toBe(true);
		expect(isProblem(ev({ level: 'ERROR' }))).toBe(true);
		expect(isProblem(ev({ kind: 'auth' }))).toBe(true);
		expect(isProblem(ev({ kind: 'error' }))).toBe(true);
		expect(isProblem(ev({ kind: 'progress' }))).toBe(false);
	});
});

function goFiles(dir: string, out: string[] = []): string[] {
	for (const name of readdirSync(dir)) {
		if (name === 'node_modules' || name === '.git' || name === 'web' || name === '.superpowers') continue;
		const p = join(dir, name);
		if (statSync(p).isDirectory()) goFiles(p, out);
		else if (name.endsWith('.go') && !name.endsWith('_test.go')) out.push(p);
	}
	return out;
}

describe('MSG_KEYS', () => {
	it('every MSG_KEYS literal exists in Go source', () => {
		const root = join(dirname(fileURLToPath(import.meta.url)), '../../..');
		const src = goFiles(root)
			.map((f) => readFileSync(f, 'utf8'))
			.join('\n');
		for (const msg of Object.keys(MSG_KEYS)) expect(src, msg).toContain(`"${msg}"`);
	});
});

import { readdirSync, readFileSync, statSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { afterEach, describe, expect, it } from 'vitest';
import type { HealthEvent } from '$lib/api/types';
import { setLang } from '$lib/i18n/index.svelte';
import { MSG_KEYS, countFilters, eventText, isProblem, kindChip, kindLabel, matchesFilter } from './health';

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

describe('kindChip', () => {
	it('maps each known kind to its label and tone', () => {
		const want: [string, string, string][] = [
			['claim', 'Claim', 'ok'],
			['progress', 'Progress', 'info'],
			['watch', 'Watch', 'info'],
			['state', 'State', 'off'],
			['discovery', 'Discovery', 'violet'],
			['error', 'Error', 'bad'],
			['auth', 'Auth', 'warn'],
			['info', 'Info', 'off'],
			['heartbeat', 'Heartbeat', 'off']
		];
		for (const [kind, label, tone] of want) expect(kindChip(ev({ kind })), kind).toEqual({ label, tone });
	});
	it('WARN reads amber and ERROR red whatever the kind', () => {
		expect(kindChip(ev({ kind: 'claim', level: 'WARN' }))).toEqual({ label: 'Claim', tone: 'warn' });
		expect(kindChip(ev({ kind: 'watch', level: 'ERROR' }))).toEqual({ label: 'Watch', tone: 'bad' });
		expect(kindChip(ev({ kind: 'error', level: 'WARN' })).tone).toBe('bad');
	});
	it('unknown kind keeps its raw name on a neutral chip', () => {
		expect(kindChip(ev({ kind: 'mystery' }))).toEqual({ label: 'mystery', tone: 'off' });
		expect(kindChip(ev({ kind: 'toString' }))).toEqual({ label: 'toString', tone: 'off' });
	});
	it('no kind is labelled by level', () => {
		expect(kindChip(ev({ kind: '' }))).toEqual({ label: 'Info', tone: 'off' });
		expect(kindChip(ev({ kind: '', level: 'WARN' }))).toEqual({ label: 'Warning', tone: 'warn' });
		expect(kindChip(ev({ kind: '', level: 'ERROR' }))).toEqual({ label: 'Error', tone: 'bad' });
	});
	it('label follows the language', () => {
		setLang('es');
		expect(kindChip(ev({ kind: 'claim' })).label).toBe('Reclamo');
	});
});

describe('matchesFilter', () => {
	it('all keeps everything, problems keeps WARN/ERROR/failed auth/error, claims keeps claim kind', () => {
		const claim = ev({ kind: 'claim' });
		const failedClaim = ev({ kind: 'claim', level: 'WARN' });
		const auth = ev({ kind: 'auth', fields: { ok: 'false' } });
		const state = ev({ kind: 'state' });
		for (const e of [claim, failedClaim, auth, state]) expect(matchesFilter('all', e)).toBe(true);
		expect([claim, failedClaim, auth, state].map((e) => matchesFilter('problems', e))).toEqual([false, true, true, false]);
		expect([claim, failedClaim, auth, state].map((e) => matchesFilter('claims', e))).toEqual([true, true, false, false]);
	});
});

describe('countFilters', () => {
	it('counts each line under every filter it matches', () => {
		const lines = [ev({ kind: 'claim' }), ev({ kind: 'claim', level: 'ERROR' }), ev({ kind: 'state' })];
		expect(countFilters(lines)).toEqual({ all: 3, problems: 1, claims: 2 });
	});
	it('each count equals the length of its filtered list', () => {
		const lines = [ev({ kind: 'claim' }), ev({ kind: 'auth', fields: { ok: 'true' } }), ev({ kind: 'error' }), ev({ kind: 'info' })];
		const c = countFilters(lines);
		for (const f of ['all', 'problems', 'claims'] as const) expect(c[f]).toBe(lines.filter((e) => matchesFilter(f, e)).length);
	});
	it('DEBUG lines are not counted', () => {
		expect(countFilters([ev({ kind: 'claim', level: 'DEBUG' })])).toEqual({ all: 0, problems: 0, claims: 0 });
	});
});

describe('sign-in check result', () => {
	it('shows pass or fail and colours the chip', () => {
		expect(eventText(ev({ kind: 'auth', msg: 'authcheck', fields: { ok: 'true' } }))).toBe('Sign-in OK');
		expect(eventText(ev({ kind: 'auth', msg: 'authcheck', fields: { ok: 'false' } }))).toBe('Sign-in check failed');
		expect(kindChip(ev({ kind: 'auth', msg: 'authcheck', fields: { ok: 'true' } })).tone).toBe('ok');
		expect(kindChip(ev({ kind: 'auth', msg: 'authcheck', fields: { ok: 'false' } })).tone).toBe('bad');
	});
});

describe('new log lines read as plain text', () => {
	it('kick ws watch and authcheck no longer show raw', () => {
		expect(eventText(ev({ kind: 'watch', msg: 'kick ws watch started', fields: { channel: 'xqc' } }))).toBe('Started watching xqc');
		expect(eventText(ev({ kind: 'auth', msg: 'authcheck' }))).toBe('Sign-in checked');
		expect(eventText(ev({ kind: 'state', msg: 'runner state change', fields: { prev: 'starting', state: 'watching' } }))).toBe(
			'Starting → Watching'
		);
	});
});

describe('isProblem', () => {
	it('flags WARN, ERROR and failed auth, not a passing sign-in check or info progress', () => {
		expect(isProblem(ev({ level: 'WARN' }))).toBe(true);
		expect(isProblem(ev({ level: 'ERROR' }))).toBe(true);
		expect(isProblem(ev({ kind: 'auth', fields: { ok: 'false' } }))).toBe(true);
		expect(isProblem(ev({ kind: 'auth', level: 'WARN' }))).toBe(true);
		expect(isProblem(ev({ kind: 'auth', fields: { ok: 'true' } }))).toBe(false);
		expect(isProblem(ev({ kind: 'auth' }))).toBe(false);
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

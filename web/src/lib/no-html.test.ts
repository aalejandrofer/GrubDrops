import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';
import { expect, test } from 'vitest';

function walk(dir: string): string[] {
	return readdirSync(dir).flatMap((name) => {
		const p = join(dir, name);
		return statSync(p).isDirectory() ? walk(p) : [p];
	});
}

test('no {@html} anywhere in src', () => {
	const files = walk('src').filter((f) => f.endsWith('.svelte'));
	const bad = files.filter((f) => readFileSync(f, 'utf8').includes('{@html'));
	expect(bad).toEqual([]);
});

// Svelte throws each_key_duplicate on a repeated key, in production too. A
// list keyed by its own item text crashes on two equal entries, so only
// lists that cannot repeat may do it.
test('each blocks keyed by the bare item are lists that cannot repeat', () => {
	const unique = new Set(['ICONS[name]', 'SECTIONS', 'games', 'scanOptions', 'canaryIntervals']);
	const re = /\{#each\s+([\w.[\]]+)\s+as\s+(\w+)\s*\(\2\)\s*\}/g;
	const bad: string[] = [];
	for (const f of walk('src').filter((f) => f.endsWith('.svelte'))) {
		for (const m of readFileSync(f, 'utf8').matchAll(re)) {
			if (!unique.has(m[1])) bad.push(`${f}: ${m[0]}`);
		}
	}
	expect(bad).toEqual([]);
});

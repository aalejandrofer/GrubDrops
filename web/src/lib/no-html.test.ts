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

import { existsSync, mkdtempSync, statSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { expect, test } from 'vitest';
import { ensureGitkeep } from './keep-dist.mjs';

test('ensureGitkeep recreates .gitkeep', () => {
	const dir = mkdtempSync(join(tmpdir(), 'keep-dist-'));
	ensureGitkeep(dir);
	expect(existsSync(join(dir, '.gitkeep'))).toBe(true);
	expect(statSync(join(dir, '.gitkeep')).size).toBe(0);
	expect(() => ensureGitkeep(dir)).not.toThrow();
});

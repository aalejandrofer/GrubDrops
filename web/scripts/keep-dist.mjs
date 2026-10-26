import { closeSync, mkdirSync, openSync } from 'node:fs';
import { join } from 'node:path';
import { fileURLToPath } from 'node:url';

// adapter-static empties the output dir; git tracks only .gitkeep there.
/** @param {string} dir */
export function ensureGitkeep(dir) {
	mkdirSync(dir, { recursive: true });
	closeSync(openSync(join(dir, '.gitkeep'), 'a'));
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
	ensureGitkeep(fileURLToPath(new URL('../../internal/web/dist', import.meta.url)));
}

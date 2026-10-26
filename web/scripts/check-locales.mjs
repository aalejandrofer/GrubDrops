import { readdirSync, readFileSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { diffKeys, referencedKeys, unusedKeys } from './locales-lib.mjs';

const SKIP = new Set(['node_modules', '.git', '.svelte-kit', 'dist', '.superpowers']);

/** @param {string} dir @returns {Generator<string>} */
function* walk(dir) {
	for (const e of readdirSync(dir, { withFileTypes: true })) {
		if (SKIP.has(e.name)) continue;
		const p = join(dir, e.name);
		if (e.isDirectory()) yield* walk(p);
		else yield p;
	}
}

/** @param {string} dir @returns {Record<string, Record<string, string>>} */
export function loadLocales(dir) {
	/** @type {Record<string, Record<string, string>>} */
	const out = {};
	for (const f of readdirSync(dir).filter((n) => n.endsWith('.json')))
		out[f.slice(0, -5)] = JSON.parse(readFileSync(join(dir, f), 'utf8'));
	return out;
}

/** @param {string} p */
function isNonProduction(p) {
	return p.endsWith('.test.ts') || p.split('/').includes('fixtures');
}

/** Text to scan: every web/src ts and svelte file, plus the `i18n.T(...)` calls of Go files outside web/. */
/** @param {string} root @returns {string[]} */
export function loadSources(root) {
	const out = [];
	for (const p of walk(join(root, 'web', 'src')))
		if (/\.(ts|svelte)$/.test(p) && !isNonProduction(p)) out.push(readFileSync(p, 'utf8'));
	for (const p of walk(root)) {
		if (!p.endsWith('.go') || p.startsWith(join(root, 'web') + '/')) continue;
		for (const m of readFileSync(p, 'utf8').matchAll(/i18n\.T\([^)]*\)/g)) out.push(m[0]);
	}
	return out;
}

/** @param {string} root @returns {string[]} */
export function checkLocales(root) {
	const locales = loadLocales(join(root, 'internal', 'i18n', 'locales'));
	const problems = diffKeys(locales);
	const refs = referencedKeys(loadSources(root));
	for (const k of unusedKeys(Object.keys(locales.en ?? {}), refs)) problems.push(`unused ${k}`);
	return problems;
}

if (process.argv[1] === fileURLToPath(import.meta.url)) {
	const problems = checkLocales(join(dirname(fileURLToPath(import.meta.url)), '..', '..'));
	for (const p of problems) console.error(p);
	process.exit(problems.length ? 1 : 0);
}

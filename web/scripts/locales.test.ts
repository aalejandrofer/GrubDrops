import { mkdirSync, mkdtempSync, readFileSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';
import { expect, test } from 'vitest';
import { diffKeys, placeholders, referencedKeys, unusedKeys } from './locales-lib.mjs';
import { loadLocales, loadSources } from './check-locales.mjs';

test('unequal key sets fail', () => {
	const p = diffKeys({ en: { 'a.b': 'x' }, es: {}, 'zh-CN': { 'a.b': 'x' } });
	expect(p).toContain('es: missing a.b');
});

test('extra keys fail', () => {
	const p = diffKeys({ en: {}, es: { 'a.b': 'x' } });
	expect(p).toContain('es: extra a.b');
});

test('placeholder mismatch fails', () => {
	const p = diffKeys({ en: { 'a.b': '{n} s ago' }, es: { 'a.b': 'hace {m} s' } });
	expect(p).toContain('es: a.b placeholders');
});

test('printf verbs compared', () => {
	expect(placeholders('%s {n} %d %%')).toEqual(['%%', '%d', '%s', '{n}']);
	expect(diffKeys({ en: { k: '%s %d' }, es: { k: '%d' } })).toContain('es: k placeholders');
});

test('unused key fails', () => {
	expect(unusedKeys(['x.y', 'a.b'], new Set(['a.b']))).toEqual(['x.y']);
});

test('tn refs cover .one and .other', () => {
	const refs = referencedKeys([`tn('x', 3)`, `t("y.z")`, `i18n.T(lang, "summary.title")`]);
	expect(refs.has('x.one') && refs.has('x.other')).toBe(true);
	expect(refs.has('y.z')).toBe(true);
	expect(refs.has('summary.title')).toBe(true);
});

test('the real locales pass', () => {
	const here = dirname(fileURLToPath(import.meta.url));
	const locales = loadLocales(join(here, '../../internal/i18n/locales'));
	expect(Object.keys(locales).sort()).toEqual(['en', 'es', 'tr', 'zh-CN']);
	const problems = diffKeys(locales);
	const refs = referencedKeys(loadSources(join(here, '../..')));
	problems.push(...unusedKeys(Object.keys(locales.en), refs).map((k) => `unused ${k}`));
	expect(problems).toEqual([]);
	expect(readFileSync(join(here, 'locales-lib.mjs'), 'utf8').length).toBeGreaterThan(0);
});

test('printf flags and widths are placeholders', () => {
	expect(placeholders('%5.2f %-3d %x')).toEqual(['%-3d', '%5.2f', '%x']);
});

test('plural categories the locale lacks are not missing', () => {
	const en = { 'n.one': '{n} item', 'n.other': '{n} items' };
	expect(diffKeys({ en, 'zh-CN': { 'n.other': '{n} 项' } })).toEqual([]);
	expect(diffKeys({ en, es: { 'n.other': '{n} elementos' } })).toContain('es: missing n.one');
	expect(diffKeys({ en, 'zh-CN': { 'n.one': '{n} 项' } })).toContain('zh-CN: missing n.other');
});

test('keys referenced only from tests or fixtures are unused', () => {
	const root = mkdtempSync(join(tmpdir(), 'locales-'));
	const src = join(root, 'web', 'src');
	mkdirSync(join(src, 'lib', 'fixtures'), { recursive: true });
	writeFileSync(join(src, 'a.test.ts'), `t('only.test')`);
	writeFileSync(join(src, 'lib', 'fixtures', 'f.ts'), `t('only.fixture')`);
	writeFileSync(join(src, 'b.ts'), `t('used.key')`);
	const refs = referencedKeys(loadSources(root));
	expect(unusedKeys(['only.test', 'only.fixture', 'used.key'], refs)).toEqual([
		'only.test',
		'only.fixture'
	]);
});

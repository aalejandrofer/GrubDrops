// Pure helpers for the locale check. No file access here.

/**
 * Sorted `{name}` placeholders plus printf verbs (%s %d %v %q %%).
 * @param {string} s
 * @returns {string[]}
 */
export function placeholders(s) {
	const out = [];
	for (const m of s.matchAll(/\{[A-Za-z_][A-Za-z0-9_]*\}|%[-+# 0-9.]*[a-zA-Z%]/g)) out.push(m[0]);
	return out.sort();
}

/**
 * @param {string} lang
 * @returns {Set<string>}
 */
function pluralCategories(lang) {
	return new Set(new Intl.PluralRules(lang).resolvedOptions().pluralCategories);
}

/**
 * True when `key` is `<prefix>.<category>`, the locale has no such category and still has `<prefix>.other`.
 * @param {string} key
 * @param {Record<string, string>} msgs
 * @param {Set<string>} cats
 * @returns {boolean}
 */
function unneededPlural(key, msgs, cats) {
	const m = /^(.+)\.(zero|one|two|few|many)$/.exec(key);
	return !!m && !cats.has(m[2]) && `${m[1]}.other` in msgs;
}

/**
 * Problems as text: missing keys, extra keys and placeholder mismatches against `en`.
 * @param {Record<string, Record<string, string>>} locales
 * @returns {string[]}
 */
export function diffKeys(locales) {
	const base = locales.en ?? {};
	const problems = [];
	for (const [lang, msgs] of Object.entries(locales)) {
		if (lang === 'en') continue;
		const cats = pluralCategories(lang);
		for (const k of Object.keys(base)) {
			if (!(k in msgs)) {
				if (!unneededPlural(k, msgs, cats)) problems.push(`${lang}: missing ${k}`);
			}
			else if (placeholders(base[k]).join(' ') !== placeholders(msgs[k]).join(' '))
				problems.push(`${lang}: ${k} placeholders`);
		}
		for (const k of Object.keys(msgs)) if (!(k in base)) problems.push(`${lang}: extra ${k}`);
	}
	return problems;
}

/**
 * Every quoted string literal in `sources`; `tn('x', ...)` also yields `x.one` and `x.other`.
 * @param {string[]} sources
 * @returns {Set<string>}
 */
export function referencedKeys(sources) {
	const refs = new Set();
	for (const src of sources) {
		for (const m of src.matchAll(/(['"`])((?:\\.|(?!\1)[^\\\n])*)\1/g)) refs.add(m[2]);
		for (const m of src.matchAll(/\btn\(\s*(['"`])([^'"`]+)\1/g)) {
			refs.add(`${m[2]}.one`);
			refs.add(`${m[2]}.other`);
		}
	}
	return refs;
}

/**
 * @param {string[]} keys
 * @param {Set<string>} refs
 * @returns {string[]}
 */
export function unusedKeys(keys, refs) {
	return keys.filter((k) => !refs.has(k));
}

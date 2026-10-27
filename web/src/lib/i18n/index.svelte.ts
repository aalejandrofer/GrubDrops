import en from '$locales/en.json';
import es from '$locales/es.json';
import zhCN from '$locales/zh-CN.json';
import tr from '$locales/tr.json';

export type Lang = 'en' | 'es' | 'zh-CN' | 'tr';
export type Vars = Record<string, string | number>;

export const LANGS: { code: Lang; label: string }[] = [
	{ code: 'en', label: 'English' },
	{ code: 'es', label: 'Español' },
	{ code: 'zh-CN', label: '简体中文' },
	{ code: 'tr', label: 'Türkçe' }
];

/** Loaded messages. Exported so tests can add plural keys. */
export const messages: Record<Lang, Record<string, string>> = {
	en: { ...en },
	es: { ...es },
	'zh-CN': { ...zhCN },
	tr: { ...tr }
};

export const i18n: { lang: Lang } = $state({ lang: 'en' });

export function setLang(l: Lang): void {
	i18n.lang = l;
	if (typeof document !== 'undefined') document.documentElement.lang = l;
}

export function matchLang(prefs: readonly string[]): Lang {
	for (const p of prefs) {
		const s = p.toLowerCase();
		if (s.startsWith('zh')) return 'zh-CN';
		if (s.startsWith('es')) return 'es';
		if (s.startsWith('tr')) return 'tr';
		if (s.startsWith('en')) return 'en';
	}
	return 'en';
}

function fill(s: string, vars?: Vars): string {
	if (!vars) return s;
	return s.replace(/\{(\w+)\}/g, (m, k: string) => (k in vars ? String(vars[k]) : m));
}

function lookup(key: string): string | undefined {
	return messages[i18n.lang][key] ?? messages.en[key];
}

export function t(key: string, vars?: Vars): string {
	return fill(lookup(key) ?? key, vars);
}

export function tn(key: string, n: number, vars?: Vars): string {
	const cat = new Intl.PluralRules(i18n.lang).select(n);
	const s = lookup(`${key}.${cat}`) ?? lookup(`${key}.other`) ?? key;
	return fill(s, { ...vars, n: new Intl.NumberFormat(i18n.lang).format(n) });
}

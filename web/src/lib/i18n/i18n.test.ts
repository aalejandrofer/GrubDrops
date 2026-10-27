import { beforeEach, expect, test } from 'vitest';
import { matchLang, messages, setLang, t, tn } from './index.svelte';

beforeEach(() => {
	setLang('en');
	messages.en['test.only_en'] = 'only en';
	for (const l of ['en', 'es', 'zh-CN', 'tr'] as const) {
		delete messages[l]['test.item.one'];
		delete messages[l]['test.item.other'];
	}
	messages.en['test.item.one'] = '{n} item';
	messages.en['test.item.other'] = '{n} items';
	messages.es['test.item.one'] = '{n} elemento';
	messages.es['test.item.other'] = '{n} elementos';
	messages['zh-CN']['test.item.other'] = '{n} 项';
	messages.tr['test.item.one'] = '{n} öğe';
	messages.tr['test.item.other'] = '{n} öğe';
});

test('t interpolates {n}', () => {
	expect(t('time.s_ago', { n: 41 })).toBe('41 s ago');
});

test('t falls back to en then key', () => {
	setLang('es');
	expect(t('test.only_en')).toBe('only en');
	expect(t('no.such.key')).toBe('no.such.key');
	expect(t('summary.title')).toBe('Resumen diario');
});

test('tn picks one/other', () => {
	expect(tn('test.item', 1)).toBe('1 item');
	expect(tn('test.item', 2)).toBe('2 items');
	setLang('es');
	expect(tn('test.item', 1)).toBe('1 elemento');
	expect(tn('test.item', 2)).toBe('2 elementos');
	setLang('zh-CN');
	expect(tn('test.item', 1)).toBe('1 项');
	setLang('tr');
	expect(tn('test.item', 2)).toBe('2 öğe');
});

test('tn falls back to other', () => {
	setLang('es');
	expect(new Intl.PluralRules('es').select(1000000)).toBe('many');
	expect(tn('test.item', 1000000)).toBe('1.000.000 elementos');
});

test('matchLang', () => {
	expect(matchLang(['zh-Hans-CN'])).toBe('zh-CN');
	expect(matchLang(['es-MX', 'en'])).toBe('es');
	expect(matchLang(['tr-TR'])).toBe('tr');
	expect(matchLang(['fr'])).toBe('en');
});

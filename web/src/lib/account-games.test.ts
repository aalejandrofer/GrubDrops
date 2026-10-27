import { describe, expect, it } from 'vitest';
import { addGame, sameList, startList } from './account-games';

describe('startList', () => {
	it('keeps a stored own list', () => {
		const own = { own: false, games: [{ id: 'g2', name: 'Two' }] };
		expect(startList(own, [{ id: 'g1', name: 'One' }])).toEqual([{ id: 'g2', name: 'Two' }]);
	});
	it('copies the global list when there is none', () => {
		const got = startList({ own: false, games: [] }, [{ id: 'g1', name: 'One' }]);
		expect(got).toEqual([{ id: 'g1', name: 'One' }]);
	});
});

describe('addGame', () => {
	const list = [{ id: 'g1', name: 'Apex Legends' }];
	it('appends a new game, trimmed', () => {
		expect(addGame(list, { id: '', name: '  Rust ' })).toEqual([...list, { id: '', name: 'Rust' }]);
	});
	it('skips a game already listed by id or by name', () => {
		expect(addGame(list, { id: 'g1', name: 'Other' })).toBe(list);
		expect(addGame(list, { id: '', name: 'apex  legends' })).toBe(list);
		expect(addGame(list, { id: '', name: '   ' })).toBe(list);
	});
});

describe('sameList', () => {
	it('compares order and names', () => {
		const a = [{ id: 'g1', name: 'One' }, { id: '', name: 'Two' }];
		expect(sameList(a, [{ id: 'g1', name: 'One' }, { id: '', name: 'two' }])).toBe(true);
		expect(sameList(a, [a[1], a[0]])).toBe(false);
		expect(sameList(a, a.slice(0, 1))).toBe(false);
	});
});

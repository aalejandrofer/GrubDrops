import { describe, expect, it } from 'vitest';
import type { ClaimsResp, GamesResp, Overview, Runner, Session } from '$lib/api/types';
import session from './session.json';
import mining from './overview.mining.json';
import runnersMining from './runners.mining.json';
import runnersMany from './runners.many.json';
import games from './games.json';
import claims from './claims.json';

describe('fixtures', () => {
	it('match the wire types and the brief counts', () => {
		const s: Session = session as Session;
		const o: Overview = mining as Overview;
		const rm: Runner[] = runnersMining as Runner[];
		const many: Runner[] = runnersMany as Runner[];
		const g: GamesResp = games as GamesResp;
		const c: ClaimsResp = claims as ClaimsResp;
		expect(s.authed).toBe(true);
		expect(o.status.state).toBe('mining');
		expect(rm).toHaveLength(3);
		expect(rm.filter((r) => r.state === 'watching').map((r) => r.current?.minutes)).toEqual([42, 18]);
		expect(rm.filter((r) => r.state === 'watching').every((r) => r.up_next.length === 2)).toBe(true);
		expect(rm.filter((r) => r.state === 'needs_login')).toHaveLength(1);
		expect(many).toHaveLength(10);
		expect(many.filter((r) => r.state === 'watching')).toHaveLength(8);
		expect(many.filter((r) => r.state === 'idle')).toHaveLength(2);
		expect(g.games.map((x) => x.name)).toEqual(['Rust', 'Marvel Rivals', 'Apex Legends', 'Delta Force']);
		expect(c.total).toBe(27);
		expect(c.items).toHaveLength(10);
	});
});

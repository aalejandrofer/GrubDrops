export type Variant = {
	id: string;
	name: string;
	note: string;
	tokens: Record<string, string>;
};

// Shared across the variants: only surfaces, borders, text and status colours move.
const common = {
	'--green-soft': 'rgba(26, 127, 55, 0.15)',
	'--amber-soft': 'rgba(154, 103, 0, 0.15)',
	'--red-soft': 'rgba(209, 36, 47, 0.15)',
	'--blue-soft': 'rgba(9, 105, 218, 0.15)',
	'--accent-soft': 'rgba(130, 80, 223, 0.14)'
};

export const variants: Variant[] = [
	{
		id: 'current',
		name: 'Current (baseline)',
		note: 'Shipped light theme, for comparison.',
		tokens: {
			'--bg': '#ffffff', '--surface': '#f6f8fa', '--surface-2': '#eaeef2', '--surface-3': '#d0d7de',
			'--border': '#d0d7de', '--border-soft': '#d8dee4', '--text': '#1f2328', '--muted': '#59636e',
			'--dim': '#6e7781', '--t2': '#424a53', '--t3': '#59636e', '--accent': '#8250df',
			'--accent-strong': '#6639ba', '--green': '#1a7f37', '--amber': '#9a6700', '--red': '#d1242f',
			'--blue': '#0969da', '--twitch-fg': '#7d3ee0', '--kick-fg': '#217a00', '--art-bg': '#d0d7de',
			...common
		}
	},
	{
		id: 'v1',
		name: 'V1 Dimmed off-white',
		note: 'Same neutral grey family as today, one step darker. Smallest change.',
		tokens: {
			'--bg': '#f1f3f5', '--surface': '#e8ebee', '--surface-2': '#dde1e5', '--surface-3': '#c9ced4',
			'--border': '#c2c8cf', '--border-soft': '#d0d5db', '--text': '#1f2328', '--muted': '#4d5762',
			'--dim': '#5c6671', '--t2': '#3a424b', '--t3': '#4d5762', '--accent': '#7040cc',
			'--accent-strong': '#5a2fb0', '--green': '#146a2b', '--amber': '#7f5600', '--red': '#b81d27',
			'--blue': '#0757b4', '--twitch-fg': '#7338d4', '--kick-fg': '#1d6f00', '--art-bg': '#c9ced4',
			...common
		}
	},
	{
		id: 'v2',
		name: 'V2 Warm paper',
		note: 'Warm grey, easy on the eyes at night. Noticeably darker than V1.',
		tokens: {
			'--bg': '#ebe7df', '--surface': '#e0dbd2', '--surface-2': '#d5cfc4', '--surface-3': '#c4bdb1',
			'--border': '#bdb5a8', '--border-soft': '#cdc6ba', '--text': '#221f1b', '--muted': '#4f4a43',
			'--dim': '#5e5951', '--t2': '#3b362f', '--t3': '#4f4a43', '--accent': '#6537bb',
			'--accent-strong': '#552ea8', '--green': '#135f28', '--amber': '#744d00', '--red': '#a91c26',
			'--blue': '#065098', '--twitch-fg': '#6c34c8', '--kick-fg': '#1a6600', '--art-bg': '#c4bdb1',
			...common
		}
	},
	{
		id: 'v3',
		name: 'V3 Cool grey-blue',
		note: 'Blue-grey, closest to the dark theme hue. Darkest of the three.',
		tokens: {
			'--bg': '#dde2ea', '--surface': '#d1d8e2', '--surface-2': '#c5cdd9', '--surface-3': '#b3bccb',
			'--border': '#a9b3c3', '--border-soft': '#bcc5d2', '--text': '#141a24', '--muted': '#3d485a',
			'--dim': '#4b5668', '--t2': '#2a3444', '--t3': '#3d485a', '--accent': '#6032b4',
			'--accent-strong': '#502a9e', '--green': '#0f5f26', '--amber': '#744d00', '--red': '#aa1a24',
			'--blue': '#054fa8', '--twitch-fg': '#5f2db8', '--kick-fg': '#175c00', '--art-bg': '#b3bccb',
			...common
		}
	}
];

export function styleOf(v: Variant): string {
	return Object.entries(v.tokens)
		.map(([k, val]) => `${k}:${val}`)
		.join(';');
}

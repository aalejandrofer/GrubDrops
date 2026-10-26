export type LoginState = 'default' | 'error' | 'sso' | 'loading' | 'setup';

export const states: { id: LoginState; name: string }[] = [
	{ id: 'default', name: 'Default (password + SSO)' },
	{ id: 'error', name: 'Error (wrong password)' },
	{ id: 'sso', name: 'SSO only' },
	{ id: 'loading', name: 'Loading (signing in)' },
	{ id: 'setup', name: 'First admin setup' }
];

export type Variant = {
	id: string;
	name: string;
	note: string;
	/** Smallest frame width that shows the layout as intended. */
	min: number;
};

export const variants: Variant[] = [
	{
		id: 'a',
		name: 'A Card',
		note: 'Today’s centered card, tightened. Password first, SSO under an “or” rule.',
		min: 380
	},
	{
		id: 'b',
		name: 'B Split',
		note: 'Two panes. Instance facts on the left, form on the right. SSO is the primary button when configured.',
		min: 680
	},
	{
		id: 'c',
		name: 'C Bare page',
		note: 'No card. App top bar, open form on the page with a larger heading, miner status and version in the footer.',
		min: 380
	}
];

// Illustrative values for the mockup.
export const fake = {
	provider: 'Authentik',
	host: 'drops.home.lan',
	version: 'v2.0.0'
};

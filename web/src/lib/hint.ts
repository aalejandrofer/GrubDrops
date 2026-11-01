// Pure helpers for Hint examples that fill a field.

/** Start and end of the last path segment of a link, so typing a game or channel name replaces it. */
export function slugRange(url: string): [number, number] {
	const end = url.replace(/[/?#]+$/, '').length;
	const start = url.lastIndexOf('/', end - 1) + 1;
	return [start, end];
}

/** The proxy field with its scheme swapped for `scheme`; the rest of the URL stays. */
export function withScheme(current: string, scheme: string): string {
	return scheme + current.trim().replace(/^[a-z][a-z0-9+.-]*:\/\//i, '');
}

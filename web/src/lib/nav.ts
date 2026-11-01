/**
 * newIn reports whether a nav link added in minor (e.g. "2.3") still shows
 * its New pill: only while the running version is that minor, so it drops
 * by itself on the next minor release.
 */
export function newIn(version: string, minor: string): boolean {
	const m = /^v?(\d+\.\d+)\./.exec(version.trim());
	return m !== null && m[1] === minor;
}

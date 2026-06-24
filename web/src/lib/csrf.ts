// readCookie returns the value of the named cookie, or null if absent.
export function readCookie(name: string): string | null {
  const prefix = name + '=';
  for (const part of document.cookie.split(';')) {
    const c = part.trim();
    if (c.startsWith(prefix)) return decodeURIComponent(c.slice(prefix.length));
  }
  return null;
}

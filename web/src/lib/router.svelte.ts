// SPA-owned paths (client-routed). Grows as pages are ported. A path NOT
// here is left to a full browser navigation (strangler: legacy HTMX page).
export const spaPaths: string[] = ['/', '/drops', '/priority', '/settings', '/settings/notifications', '/settings/experimental', '/settings/proxy'];

export function isSpaPath(path: string): boolean {
  return spaPaths.includes(path);
}

let path = $state(typeof location !== 'undefined' ? location.pathname : '/');

export function currentPath(): string {
  return path;
}

export function navigate(to: string): void {
  const u = new URL(to, location.href);
  const full = u.pathname + u.search + u.hash;
  history.pushState({}, '', full);
  path = u.pathname; // route-matching uses pathname only
}

function onPopState(): void {
  path = location.pathname;
}

function onClick(e: MouseEvent): void {
  if (e.button !== 0 || e.metaKey || e.ctrlKey || e.shiftKey || e.altKey || e.defaultPrevented) return;
  const a = (e.target as Element | null)?.closest('a');
  if (!a) return;
  const href = a.getAttribute('href');
  if (!href || a.target === '_blank' || a.hasAttribute('download')) return;
  // same-origin only
  const url = new URL(href, location.href);
  if (url.origin !== location.origin) return;
  if (!isSpaPath(url.pathname)) return; // unowned → let the browser navigate (legacy)
  e.preventDefault();
  navigate(url.pathname + url.search + url.hash);
}

// startRouter attaches the popstate + click interceptor; returns a teardown.
export function startRouter(): () => void {
  window.addEventListener('popstate', onPopState);
  document.addEventListener('click', onClick);
  return () => {
    window.removeEventListener('popstate', onPopState);
    document.removeEventListener('click', onClick);
  };
}

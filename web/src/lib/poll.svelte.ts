// pollingResource is a reactive polling primitive: it fetches fn()
// immediately, then every intervalMs, pausing while the tab is hidden and
// refetching as soon as it becomes visible again. A failed fetch records
// error but leaves the last successful value in place, so a transient blip
// never blanks the UI. State is held in Svelte 5 runes (hence the
// .svelte.ts extension) so consumers re-render when current/error change.

export interface PollingResource<T> {
  readonly current: T | null;
  readonly error: Error | null;
  stop(): void;
}

export function pollingResource<T>(fn: () => Promise<T>, intervalMs: number): PollingResource<T> {
  let current = $state<T | null>(null);
  let err = $state<Error | null>(null);

  const hidden = () => typeof document !== 'undefined' && document.hidden;

  async function tick() {
    if (hidden()) return;
    try {
      current = await fn();
      err = null;
    } catch (e) {
      err = e instanceof Error ? e : new Error(String(e));
    }
  }

  function onVisibility() {
    if (!hidden()) void tick();
  }

  void tick(); // immediate first load
  const timer = setInterval(() => void tick(), intervalMs);
  if (typeof document !== 'undefined') {
    document.addEventListener('visibilitychange', onVisibility);
  }

  return {
    get current() {
      return current;
    },
    get error() {
      return err;
    },
    stop() {
      clearInterval(timer);
      if (typeof document !== 'undefined') {
        document.removeEventListener('visibilitychange', onVisibility);
      }
    },
  };
}

// Make window.location configurable so vi.spyOn(window.location, 'assign') works in jsdom.
// jsdom marks location properties as non-configurable by default; this replaces the
// whole location object with a configurable writable stand-in before each test file runs.
Object.defineProperty(window, 'location', {
  configurable: true,
  writable: true,
  value: Object.assign({}, window.location, {
    assign: () => {},
  }),
});

import '@testing-library/jest-dom/vitest'

// jsdom implements neither ResizeObserver nor matchMedia; the shell measures
// both, so the stubs keep those components mountable in unit tests.
if (!('ResizeObserver' in globalThis)) {
  globalThis.ResizeObserver = class {
    observe() {}
    unobserve() {}
    disconnect() {}
  } as unknown as typeof ResizeObserver
}

if (!window.matchMedia) {
  window.matchMedia = ((query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addEventListener() {},
    removeEventListener() {},
    dispatchEvent() {
      return false
    }
  })) as unknown as typeof window.matchMedia
}

import '@testing-library/jest-dom/vitest'
import { cleanup } from '@testing-library/react'
import { afterEach, vi } from 'vitest'

// Testing Library only auto-cleans with global afterEach; vitest runs
// without globals here, so unmount explicitly between tests.
afterEach(() => {
  cleanup()
})

// jsdom lacks ResizeObserver (react-resizable-panels) and matchMedia.
class MockResizeObserver {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}
vi.stubGlobal('ResizeObserver', MockResizeObserver)

Object.defineProperty(window, 'matchMedia', {
  writable: true,
  value: (query: string) => ({
    matches: false,
    media: query,
    onchange: null,
    addListener: () => {},
    removeListener: () => {},
    addEventListener: () => {},
    removeEventListener: () => {},
    dispatchEvent: () => false,
  }),
})

// useAppPreference syncs to /api/preferences; keep prefs local-only in tests.
vi.stubGlobal(
  'fetch',
  vi.fn(async () => ({ ok: true, status: 200, text: async () => '' })),
)

import { cleanup } from '@testing-library/react'
import { afterEach } from 'vitest'

// Every test starts with an empty page, so nothing leaks from one test into the next.
afterEach(() => {
  cleanup()
})

// The pretend browser used in tests cannot measure elements, and charts ask for that when they are drawn.
class ResizeObserverStub {
  observe(): void {}
  unobserve(): void {}
  disconnect(): void {}
}
globalThis.ResizeObserver ??= ResizeObserverStub

import { act } from 'react'
import { afterEach, vi } from 'vitest'

// React 19 + react-test-renderer require an act-enabled DOM environment.
;(globalThis as { IS_REACT_ACT_ENVIRONMENT?: boolean }).IS_REACT_ACT_ENVIRONMENT = true

// Ensure createPortal targets exist under jsdom.
if (typeof document !== 'undefined' && !document.getElementById('root')) {
  const root = document.createElement('div')
  root.id = 'root'
  document.body.appendChild(root)
}

// Wrap RTR create in act so concurrent React 19 doesn't leave trees unmounted
// before tests read `.root`.
vi.mock('react-test-renderer', async () => {
  const actual = await vi.importActual<typeof import('react-test-renderer')>('react-test-renderer')
  return {
    ...actual,
    create(element: Parameters<typeof actual.create>[0], options?: Parameters<typeof actual.create>[1]) {
      let renderer!: ReturnType<typeof actual.create>
      act(() => {
        renderer = actual.create(element, options)
      })
      return renderer
    },
  }
})

// Reduce pure-noise deprecation / act spam while keeping real errors.
const originalError = console.error
console.error = (...args: unknown[]) => {
  const msg = String(args[0] ?? '')
  if (msg.includes('react-test-renderer is deprecated')) return
  if (msg.includes('not wrapped in act(')) return
  originalError(...args)
}

afterEach(() => {
  // tests own their renderers
})

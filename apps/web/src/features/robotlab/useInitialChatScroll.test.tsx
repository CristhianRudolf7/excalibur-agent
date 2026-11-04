import { useRef } from 'react'
import { act, cleanup, fireEvent, render } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { useInitialChatScroll } from './useInitialChatScroll'

let callback: () => void
let disconnected: ReturnType<typeof vi.fn>
let frames: Map<number, FrameRequestCallback>
let next = 0
function tick() {
  act(() => {
    const pending = [...frames.values()]
    frames.clear()
    pending.forEach((fn) => fn(0))
  })
}
function Panel({ loaded = true, active = true }: { loaded?: boolean; active?: boolean }) {
  const ref = useRef<HTMLDivElement>(null)
  useInitialChatScroll(ref, active, 'session')
  return (
    <div ref={ref}>
      <div className="messages">{loaded && <article className="message">Latest</article>}</div>
    </div>
  )
}
beforeEach(() => {
  frames = new Map()
  disconnected = vi.fn()
  vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(1500)
  vi.stubGlobal('requestAnimationFrame', (fn: FrameRequestCallback) => {
    frames.set(++next, fn)
    return next
  })
  vi.stubGlobal('cancelAnimationFrame', (id: number) => frames.delete(id))
  vi.stubGlobal(
    'MutationObserver',
    class {
      constructor(fn: () => void) {
        callback = fn
      }
      observe() {}
      disconnect = disconnected
    },
  )
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})
it('opens at the latest message and settles after virtual row measurement', () => {
  const view = render(<Panel />)
  const log = view.container.querySelector('.messages')!
  tick()
  expect(log.scrollTop).toBe(1500)
  vi.spyOn(HTMLElement.prototype, 'scrollHeight', 'get').mockReturnValue(1900)
  callback()
  tick()
  expect(log.scrollTop).toBe(1900)
  tick()
  expect(disconnected).toHaveBeenCalledOnce()
})
it('waits for asynchronous history and cancels when the user scrolls', () => {
  const view = render(<Panel loaded={false} />)
  expect(frames.size).toBe(0)
  view.rerender(<Panel />)
  callback()
  tick()
  const log = view.container.querySelector('.messages')!
  expect(log.scrollTop).toBe(1500)
  fireEvent.wheel(log)
  expect(frames.size).toBe(0)
  expect(disconnected).toHaveBeenCalledOnce()
})
it('scrolls when the chat tab reopens and cancels pending work on unmount', () => {
  const view = render(<Panel active={false} />)
  expect(frames.size).toBe(0)
  view.rerender(<Panel />)
  expect(frames.size).toBe(1)
  view.unmount()
  expect(frames.size).toBe(0)
})

import { useLayoutEffect, type RefObject } from 'react'

/** The shared Chat skips initial history scrolling; keep this adjustment local to the lab. */
export function useInitialChatScroll(
  panel: RefObject<HTMLDivElement | null>,
  active: boolean,
  session: string,
) {
  useLayoutEffect(() => {
    const root = panel.current
    if (!root || !active) return
    let pending: number | undefined
    const stop = () => {
      observer.disconnect()
      if (pending !== undefined) cancelAnimationFrame(pending)
      pending = undefined
    }
    const reveal = () => {
      if (pending !== undefined) cancelAnimationFrame(pending)
      const log = root.querySelector<HTMLElement>('.messages')
      if (!log?.querySelector('.message')) return
      pending = requestAnimationFrame(() => {
        log.scrollTop = log.scrollHeight
        // Virtualized rows can change height after scrolling; wait for their measurements.
        pending = requestAnimationFrame(stop)
      })
    }
    const observer = new MutationObserver(reveal)
    observer.observe(root, { childList: true, subtree: true, attributes: true })
    const events = ['wheel', 'touchstart', 'pointerdown', 'keydown'] as const
    events.forEach((event) => root.addEventListener(event, stop, { passive: true }))
    reveal()
    return () => {
      stop()
      events.forEach((event) => root.removeEventListener(event, stop))
    }
  }, [panel, active, session])
}

import { useLayoutEffect, useRef, useState, type ReactNode } from 'react'
import { navigateTabs } from './tabs'

/** Measure the selected tab so translated labels keep the indicator aligned. */
export function WorkTabs({
  value,
  label,
  children,
}: {
  value: string
  label: string
  children: ReactNode
}) {
  const ref = useRef<HTMLDivElement>(null)
  const [indicator, setIndicator] = useState({ x: 0, width: 0 })
  useLayoutEffect(() => {
    const node = ref.current
    if (!node) return
    const measure = () => {
      const selected = node.querySelector<HTMLElement>('[aria-selected="true"]')
      if (selected) {
        setIndicator({ x: selected.offsetLeft, width: selected.offsetWidth })
        selected.scrollIntoView({ block: 'nearest', inline: 'nearest' })
      }
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(node)
    for (const tab of node.querySelectorAll('button')) observer.observe(tab)
    return () => observer.disconnect()
  }, [value, children])
  return (
    <div ref={ref} className="work-tabs" role="tablist" onKeyDown={navigateTabs} aria-label={label}>
      <span
        aria-hidden="true"
        className="work-tab-indicator"
        style={{ width: indicator.width, transform: `translateX(${indicator.x}px)` }}
      />
      {children}
    </div>
  )
}

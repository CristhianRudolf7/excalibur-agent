import { useRef, useState, type KeyboardEvent, type PointerEvent } from 'react'

export interface ResizableSizeOptions {
  value: number
  min: number
  max: number
  defaultValue: number
  onChange: (value: number) => void
  getPointerValue: (clientX: number) => number | null
  direction?: 1 | -1
  onReset?: () => void
  onHome?: () => void
}
/** Shared pointer capture, validation and keyboard controls for horizontal panes. */
export function useResizableSize({
  value,
  min,
  max,
  defaultValue,
  onChange,
  getPointerValue,
  direction = 1,
  onReset,
  onHome,
}: ResizableSizeOptions) {
  const pointer = useRef<number | null>(null)
  const [dragging, setDragging] = useState(false)
  const change = (size: number | null) => {
    if (size === null || !Number.isFinite(size) || size <= 0) return
    onChange(Math.max(min, Math.min(max, size)))
  }
  const end = () => {
    pointer.current = null
    setDragging(false)
  }
  return {
    dragging,
    onDoubleClick: () => (onReset ? onReset() : change(defaultValue)),
    onKeyDown: (event: KeyboardEvent<HTMLElement>) => {
      if (event.key === 'ArrowRight' || event.key === 'ArrowLeft') {
        event.preventDefault()
        change(value + (event.key === 'ArrowRight' ? 24 : -24) * direction)
      } else if (event.key === 'Home') {
        event.preventDefault()
        if (onHome) onHome()
        else change(min)
      } else if (event.key === 'End') {
        event.preventDefault()
        change(max)
      }
    },
    onPointerDown: (event: PointerEvent<HTMLElement>) => {
      if (event.button !== 0) return
      event.preventDefault()
      pointer.current = event.pointerId
      event.currentTarget.setPointerCapture(event.pointerId)
      setDragging(true)
    },
    onPointerMove: (event: PointerEvent<HTMLElement>) => {
      if (pointer.current === event.pointerId) change(getPointerValue(event.clientX))
    },
    onPointerUp: (event: PointerEvent<HTMLElement>) => {
      if (pointer.current !== event.pointerId) return
      if (event.currentTarget.hasPointerCapture(event.pointerId))
        event.currentTarget.releasePointerCapture(event.pointerId)
      end()
    },
    onPointerCancel: end,
    onLostPointerCapture: end,
  }
}

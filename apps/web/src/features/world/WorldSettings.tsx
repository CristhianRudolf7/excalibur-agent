import { useEffect, useId, useRef, useState } from 'react'
import { Settings, Minus, Plus, FastForward, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { nextSpeed } from './preferences'
import { stepZoom, zoomSteps } from './engine/camera'
/** Controlled speed/zoom controls; persists nothing itself and emits values through callbacks. */
export function WorldSettings({
  speed,
  zoom,
  onSpeed,
  onZoom,
}: {
  speed: number
  zoom: number
  onSpeed: (speed: number) => void
  onZoom: (zoom: number) => void
}) {
  const { t, i18n } = useTranslation(),
    [open, setOpen] = useState(false)
  const skipTouchClick = useRef(false)
  const root = useRef<HTMLDivElement>(null),
    trigger = useRef<HTMLButtonElement>(null),
    id = useId()
  useEffect(() => {
    if (!open) return
    root.current?.querySelector<HTMLButtonElement>('section button')?.focus()
    const outside = (event: PointerEvent) => {
      if (event.target instanceof Node && !root.current?.contains(event.target)) setOpen(false)
    }
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        event.stopPropagation()
        setOpen(false)
        trigger.current?.focus()
      }
    }
    document.addEventListener('pointerdown', outside)
    document.addEventListener('keydown', key, true)
    return () => {
      document.removeEventListener('pointerdown', outside)
      document.removeEventListener('keydown', key, true)
    }
  }, [open])
  return (
    <div className="world-settings" ref={root}>
      {open && (
        <section
          className="world-settings-popover world-settings-pill world-panel"
          id={id}
          role="toolbar"
          aria-label={t('worldSettings')}
          aria-orientation="vertical"
          onKeyDown={(event) => {
            const buttons = [
              ...event.currentTarget.querySelectorAll<HTMLButtonElement>('button:not(:disabled)'),
            ]
            const index = buttons.indexOf(document.activeElement as HTMLButtonElement)
            const next =
              event.key === 'ArrowDown'
                ? (index + 1) % buttons.length
                : event.key === 'ArrowUp'
                  ? (index + buttons.length - 1) % buttons.length
                  : event.key === 'Home'
                    ? 0
                    : event.key === 'End'
                      ? buttons.length - 1
                      : -1
            if (next >= 0) {
              event.preventDefault()
              buttons[next]?.focus()
            }
          }}
        >
          <div className="world-settings-speed">
            <button
              type="button"
              aria-label={t('worldSpeedUp')}
              title={t('worldSpeedUp')}
              onClick={() => onSpeed(nextSpeed(speed))}
            >
              <FastForward size={16} />
              <span className="world-control-tooltip" role="tooltip">
                {t('worldSpeedUp')}
              </span>
            </button>
            <output aria-live="polite" aria-label={t('worldMovementSpeed')}>
              {t('worldSpeedValue', { value: new Intl.NumberFormat(i18n.language).format(speed) })}
            </output>
          </div>
          <button
            type="button"
            aria-label={t('worldZoomIn')}
            title={t('worldZoomIn')}
            disabled={zoom >= zoomSteps.at(-1)!}
            onClick={() => onZoom(stepZoom(zoom, 1))}
          >
            <Plus size={16} />
            <span className="world-control-tooltip" role="tooltip">
              {t('worldZoomIn')}
            </span>
          </button>
          <output className="world-pill-zoom" aria-live="polite" aria-label={t('worldZoom')}>
            {t('worldZoomPercent', { value: Math.round(zoom * 100) })}
          </output>
          <button
            type="button"
            aria-label={t('worldZoomOut')}
            title={t('worldZoomOut')}
            disabled={zoom <= zoomSteps[0]}
            onClick={() => onZoom(stepZoom(zoom, -1))}
          >
            <Minus size={16} />
            <span className="world-control-tooltip" role="tooltip">
              {t('worldZoomOut')}
            </span>
          </button>
        </section>
      )}
      <button
        className="world-settings-trigger world-panel"
        ref={trigger}
        type="button"
        aria-label={t('worldSettings')}
        title={t('worldSettings')}
        aria-expanded={open}
        aria-controls={open ? id : undefined}
        onPointerDown={() => {
          skipTouchClick.current = false
        }}
        onPointerUp={(event) => {
          if (event.pointerType === 'touch') {
            skipTouchClick.current = true
            setOpen((value) => !value)
          }
        }}
        onClick={(event) => {
          if (skipTouchClick.current && event.detail !== 0) {
            skipTouchClick.current = false
            return
          }
          skipTouchClick.current = false
          setOpen((value) => !value)
        }}
      >
        {open ? <X size={18} /> : <Settings size={18} />}
      </button>
    </div>
  )
}

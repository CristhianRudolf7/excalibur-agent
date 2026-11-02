import { useEffect, useId, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { X } from 'lucide-react'
import { PixelPortrait } from '@/components/PixelSprite'
import type { MapShape, MapMarker, MapViewport } from './Minimap'
import type { Point } from './layout'
import { minimapTransform, minimapViewport, viewportStroke } from './minimap-transform'
import { mapPoint, mapShapeAt, shapeCenter } from './map-geometry'
export interface MapModalProps {
  open: boolean
  width: number
  height: number
  background?: string
  shapes: readonly MapShape[]
  markers: readonly MapMarker[]
  viewport: MapViewport
  onClose: () => void
  onRoom: (id: string) => void
  onNavigate: (point: Point) => void
}
/** Generic full-map dialog. Shapes carry room labels, occupants and mission counts; callbacks own all navigation. */
export function MapModal({
  open,
  width,
  height,
  background,
  shapes,
  markers,
  viewport,
  onClose,
  onRoom,
  onNavigate,
}: MapModalProps) {
  const { t } = useTranslation(),
    id = useId()
  const dialog = useRef<HTMLDialogElement>(null),
    close = useRef<HTMLButtonElement>(null),
    surface = useRef<HTMLDivElement>(null)
  const previous = useRef<Element | null>(null)
  const [available, setAvailable] = useState({ width: 800, height: 600 }),
    [hovered, setHovered] = useState<string>()
  const transform = minimapTransform({ width, height })
  const fit = Math.min(available.width / width, available.height / height)
  const stageWidth = width * fit,
    stageHeight = height * fit
  const stroke = (1.5 * transform.width) / stageWidth
  const rect = viewportStroke(minimapViewport(viewport, { width, height }), stroke)
  useEffect(() => {
    const node = dialog.current!
    if (open) {
      previous.current = document.activeElement
      if (!node.open) node.showModal()
      close.current?.focus()
    } else if (node.open) {
      const timer = setTimeout(
        () => {
          node.close()
          if (previous.current instanceof HTMLElement && previous.current.isConnected)
            previous.current.focus()
        },
        window.matchMedia('(prefers-reduced-motion: reduce)').matches ? 0 : 180,
      )
      return () => clearTimeout(timer)
    }
  }, [open])
  useEffect(() => {
    if (!open || !surface.current) return
    const observer = new ResizeObserver(([entry]) => {
      if (entry)
        setAvailable({
          width: Math.max(1, entry.contentRect.width),
          height: Math.max(1, entry.contentRect.height),
        })
    })
    observer.observe(surface.current)
    return () => observer.disconnect()
  }, [open])
  useEffect(() => {
    const node = dialog.current
    return () => {
      node?.close()
    }
  }, [])
  const selectRoom = (roomId: string) => {
    onClose()
    onRoom(roomId)
  }
  return (
    <dialog
      ref={dialog}
      className="world-map-modal"
      data-state={open ? 'open' : 'closed'}
      role="dialog"
      aria-modal="true"
      aria-labelledby={id}
      onCancel={(event) => {
        event.preventDefault()
        onClose()
      }}
      onKeyDownCapture={(event) => {
        if (event.key === 'Escape') {
          event.preventDefault()
          event.stopPropagation()
          onClose()
        }
        if (event.key === 'Tab') {
          const nodes = event.currentTarget.querySelectorAll<HTMLElement>(
              'button:not(:disabled), [tabindex="0"]',
            ),
            first = nodes[0],
            last = nodes[nodes.length - 1]
          if (event.shiftKey && document.activeElement === first) {
            event.preventDefault()
            last?.focus()
          } else if (!event.shiftKey && document.activeElement === last) {
            event.preventDefault()
            first?.focus()
          }
        }
      }}
      onClick={(event) => {
        if (event.target === dialog.current) {
          const bounds = event.currentTarget.getBoundingClientRect()
          if (
            event.clientX < bounds.left ||
            event.clientX > bounds.right ||
            event.clientY < bounds.top ||
            event.clientY > bounds.bottom
          )
            onClose()
        }
      }}
    >
      <header className="world-map-modal-header">
        <h2 id={id}>{t('worldFullMap')}</h2>
        <button ref={close} type="button" aria-label={t('worldCloseMap')} onClick={onClose}>
          <X size={18} />
        </button>
      </header>
      <div className="world-map-modal-content">
        <div className="world-full-map-surface" ref={surface}>
          <div className="world-full-map-stage" style={{ width: stageWidth, height: stageHeight }}>
            <svg
              viewBox={`0 0 ${transform.width} ${transform.height}`}
              className="world-full-map-image"
              role="img"
              aria-label={t('worldCampus')}
              onPointerMove={(event) => {
                const point = mapPoint(
                  { x: event.clientX, y: event.clientY },
                  event.currentTarget.getBoundingClientRect(),
                  { width, height },
                )
                setHovered(mapShapeAt(shapes, point)?.id)
              }}
              onPointerLeave={() => setHovered(undefined)}
              onClick={(event) => {
                const point = mapPoint(
                    { x: event.clientX, y: event.clientY },
                    event.currentTarget.getBoundingClientRect(),
                    { width, height },
                  ),
                  room = mapShapeAt(shapes, point)
                if (room) selectRoom(room.id)
                else {
                  onClose()
                  onNavigate(point)
                }
              }}
            >
              {background && (
                <image
                  href={background}
                  width={transform.width}
                  height={transform.height}
                  preserveAspectRatio="none"
                />
              )}
              {shapes.map((shape) => (
                <polygon
                  key={shape.id}
                  data-map-room={shape.id}
                  className={hovered === shape.id ? 'is-hovered' : undefined}
                  points={shape.points
                    .map((point) => {
                      const p = transform.toMini(point)
                      return `${p.x},${p.y}`
                    })
                    .join(' ')}
                  style={{
                    fill: shape.color ?? 'var(--primary)',
                    stroke: shape.color ?? 'var(--primary)',
                  }}
                  strokeWidth={1}
                  vectorEffect="non-scaling-stroke"
                />
              ))}
              {markers.map((marker) => {
                const point = transform.toMini(marker.position)
                return (
                  <circle
                    key={marker.id}
                    data-world-full-marker={marker.id}
                    cx={point.x}
                    cy={point.y}
                    r={1.4}
                    fill={marker.color}
                    stroke="var(--world-marker-ring)"
                    strokeWidth={1.5}
                    vectorEffect="non-scaling-stroke"
                  />
                )
              })}
              <rect
                className="world-full-map-viewport"
                data-map-stroke={stroke}
                {...rect}
                strokeWidth={1.5}
                vectorEffect="non-scaling-stroke"
              />
            </svg>
            <div className="world-full-map-labels">
              {shapes.map((shape) => {
                const center = shapeCenter(shape)
                return (
                  <button
                    key={shape.id}
                    className="world-full-map-label"
                    style={{
                      left: `${(center.x / width) * 100}%`,
                      top: `${(center.y / height) * 100}%`,
                    }}
                    type="button"
                    aria-label={t('worldVisitRoom', { room: t(shape.label ?? shape.id) })}
                    onMouseEnter={() => setHovered(shape.id)}
                    onMouseLeave={() => setHovered(undefined)}
                    onFocus={() => setHovered(shape.id)}
                    onBlur={() => setHovered(undefined)}
                    onClick={() => selectRoom(shape.id)}
                  >
                    <strong>{t(shape.label ?? shape.id)}</strong>
                    <span>
                      {shape.agents?.map((agent) => (
                        <PixelPortrait
                          key={agent.id}
                          sprite={agent.sprite}
                          size={18}
                          label={t(agent.name)}
                        />
                      ))}
                    </span>
                  </button>
                )
              })}
            </div>
          </div>
        </div>
        <nav className="world-map-room-list" aria-label={t('worldMapRoomList')}>
          {shapes.map((shape) => (
            <button key={shape.id} type="button" onClick={() => selectRoom(shape.id)}>
              <strong>{t(shape.label ?? shape.id)}</strong>
              <span className="world-map-room-agents">
                {shape.agents?.map((agent) => (
                  <PixelPortrait
                    key={agent.id}
                    sprite={agent.sprite}
                    size={24}
                    label={t(agent.name)}
                  />
                ))}
                {!shape.agents?.length && <small>{t('worldNoAgents')}</small>}
              </span>
              <small>{t('worldActiveMissions', { count: shape.missionCount ?? 0 })}</small>
            </button>
          ))}
        </nav>
      </div>
    </dialog>
  )
}

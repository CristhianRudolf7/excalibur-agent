import { useRef, useState, type PointerEvent } from 'react'
import { useTranslation } from 'react-i18next'
import { ChevronDown, Maximize2 } from 'lucide-react'
import { hitPolygon, type Point } from './layout'
import type { SpriteDefinition } from '@/components/pixel-sprites'
import {
  minimapTransform,
  minimapViewport,
  viewportStroke,
  type Viewport,
} from './minimap-transform'
export interface MapRoomAgent {
  id: string
  name: string
  sprite: SpriteDefinition
}
export interface MapShape {
  id: string
  points: readonly Point[]
  color?: string
  label?: string
  agents?: readonly MapRoomAgent[]
  missionCount?: number
}
export interface MapMarker {
  id: string
  position: Point
  color: string
  active?: boolean
}
export type MapViewport = Viewport
/** Data-only thumbnail map. All layers share one transform; navigation emits a world center. inline defaults to document flow. */
export function Minimap({
  width,
  height,
  shapes,
  markers,
  viewport,
  background,
  onNavigate,
  onExpand,
  inline = true,
}: {
  width: number
  height: number
  shapes: readonly MapShape[]
  markers: readonly MapMarker[]
  viewport: MapViewport
  background?: string
  inline?: boolean
  onNavigate: (point: Point) => void
  onExpand?: () => void
}) {
  const { t } = useTranslation(),
    [closed, setClosed] = useState(false),
    [hovered, setHovered] = useState<string | undefined>(undefined)
  const drag = useRef<{
    id: number
    offset: Point
    start: Point
    moved: boolean
    viewport: boolean
  } | null>(null)
  const transform = minimapTransform({ width, height }),
    rect = viewportStroke(minimapViewport(viewport, { width, height }))
  const point = (event: PointerEvent<SVGSVGElement>) => {
    const bounds = event.currentTarget.getBoundingClientRect()
    return transform.toWorld({
      x: ((event.clientX - bounds.left) / bounds.width) * transform.width,
      y: ((event.clientY - bounds.top) / bounds.height) * transform.height,
    })
  }
  const move = (event: PointerEvent<SVGSVGElement>) => {
    const position = point(event),
      offset = drag.current?.offset
    onNavigate(
      offset
        ? {
            x: position.x - offset.x + viewport.width / 2,
            y: position.y - offset.y + viewport.height / 2,
          }
        : position,
    )
  }
  return (
    <section
      style={inline ? { position: 'relative', inset: 'auto' } : undefined}
      className="world-minimap world-panel"
      aria-label={t('worldMap')}
    >
      <div className="world-minimap-heading">
        <button
          className="world-panel-title"
          onClick={() => setClosed(!closed)}
          aria-expanded={!closed}
        >
          <strong>{t('worldMap')}</strong>
          <ChevronDown size={14} />
        </button>
        {onExpand && (
          <button
            className="world-map-expand"
            type="button"
            onClick={onExpand}
            aria-label={t('worldOpenFullMap')}
            title={t('worldOpenFullMap')}
          >
            <Maximize2 size={14} />
          </button>
        )}
      </div>
      {hovered && !closed && (
        <span className="world-map-tooltip" role="tooltip">
          {t(hovered)}
        </span>
      )}
      {!closed && (
        <svg
          viewBox={`0 0 ${transform.width} ${transform.height}`}
          width={transform.width}
          height={transform.height}
          style={{ aspectRatio: `${width} / ${height}` }}
          role="application"
          aria-label={t('worldMapMove')}
          tabIndex={0}
          onKeyDown={(event) => {
            if (onExpand && (event.key === 'Enter' || event.key === ' ')) {
              event.preventDefault()
              onExpand()
              return
            }
            const direction = {
              ArrowLeft: [-180, 0],
              ArrowRight: [180, 0],
              ArrowUp: [0, -180],
              ArrowDown: [0, 180],
            }[event.key]
            if (direction) {
              event.preventDefault()
              onNavigate({
                x: viewport.x + viewport.width / 2 + direction[0]!,
                y: viewport.y + viewport.height / 2 + direction[1]!,
              })
            }
          }}
          onPointerDown={(event) => {
            event.preventDefault()
            const position = point(event)
            const inside =
              event.target instanceof Element &&
              event.target.classList.contains('world-map-viewport')
            drag.current = {
              id: event.pointerId,
              start: { x: event.clientX, y: event.clientY },
              moved: false,
              viewport: inside,
              offset: inside
                ? { x: position.x - viewport.x, y: position.y - viewport.y }
                : { x: viewport.width / 2, y: viewport.height / 2 },
            }
            event.currentTarget.setPointerCapture?.(event.pointerId)
            if (!inside && !onExpand) move(event)
          }}
          onPointerLeave={() => setHovered(undefined)}
          onPointerMove={(event) => {
            const position = point(event)
            setHovered(shapes.find((shape) => hitPolygon(position, [...shape.points]))?.label)
            const current = drag.current
            if (current?.id !== event.pointerId) return
            if (Math.hypot(event.clientX - current.start.x, event.clientY - current.start.y) > 2)
              current.moved = true
            if (current.moved) move(event)
          }}
          onPointerUp={(event) => {
            if (drag.current && !drag.current.moved) {
              if (onExpand) onExpand()
              else if (drag.current.viewport) onNavigate(point(event))
            }
            drag.current = null
          }}
          onPointerCancel={() => {
            drag.current = null
          }}
        >
          {background && (
            <image
              className="world-map-thumbnail"
              href={background}
              x={0}
              y={0}
              width={transform.width}
              height={transform.height}
              preserveAspectRatio="none"
            />
          )}
          {shapes.map((shape) => (
            <polygon
              key={shape.id}
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
              vectorEffect="non-scaling-stroke"
              strokeWidth={1}
            >
              {shape.label && <title>{t(shape.label)}</title>}
            </polygon>
          ))}
          {markers.map((marker) => {
            const p = transform.toMini(marker.position)
            return (
              <circle
                key={marker.id}
                data-world-marker={marker.id}
                className={marker.active ? 'world-map-active-agent' : undefined}
                cx={p.x}
                cy={p.y}
                r={3}
                style={{ fill: marker.color }}
                vectorEffect="non-scaling-stroke"
                strokeWidth={1.5}
              />
            )
          })}
          <rect
            className="world-map-viewport"
            {...rect}
            data-world-x={viewport.x}
            data-world-y={viewport.y}
            data-world-width={viewport.width}
            data-world-height={viewport.height}
            vectorEffect="non-scaling-stroke"
            strokeWidth={1.5}
          />
        </svg>
      )}
    </section>
  )
}

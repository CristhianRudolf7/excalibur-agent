import { useEffect, useId, useMemo, useRef, useState, type CSSProperties } from 'react'
import { useTranslation } from 'react-i18next'
import { Bell, Check, Clock3, Code2, Diamond, Maximize, Minus, Plus, Wrench, X } from 'lucide-react'
import { PixelAvatar } from '@/components/shared'
import type { Automation, AutomationRun, AutomationStep } from '@/lib/rpc/contract'
import { edgePath, layoutFlow, NODE_HEIGHT, NODE_WIDTH } from './layout'
import { edgeTraversed, stepStatus } from './status'
import { stepDetail } from './format'
import './i18n'

export function StepIcon({ type }: { type: AutomationStep['type'] | 'schedule' }) {
  if (type === 'agent') return <PixelAvatar id="excalibur" size={28} />
  const Icon = { script: Code2, tool: Wrench, condition: Diamond, notify: Bell, schedule: Clock3 }[
    type
  ]
  return <Icon size={19} aria-hidden="true" />
}
export function FlowDiagram({
  automation,
  schedule,
  run,
  selected,
  onSelect,
}: {
  automation: Automation
  schedule: string
  run?: AutomationRun
  selected?: string
  onSelect: (id: string) => void
}) {
  const { t } = useTranslation('automationDetail')
  const layout = useMemo(
    () => layoutFlow(automation.steps, automation.edges),
    [automation.steps, automation.edges],
  )
  const viewport = useRef<HTMLDivElement>(null)
  const buttons = useRef(new Map<string, HTMLButtonElement>())
  const [available, setAvailable] = useState(640)
  const [manualZoom, setManualZoom] = useState<number>()
  const uid = useId().replaceAll(':', '')
  useEffect(() => {
    const element = viewport.current
    if (!element) return
    const observer = new ResizeObserver(([entry]) => {
      if (entry) setAvailable(entry.contentRect.width)
    })
    observer.observe(element)
    return () => observer.disconnect()
  }, [])
  const fit = Math.min(1, Math.max(0.2, (available - 16) / layout.width))
  const zoom = manualZoom ?? fit
  const positions = new Map(layout.nodes.map((node) => [node.id, node]))
  const steps = new Map(automation.steps.map((step) => [step.id, step]))
  const replay = run?.version === automation.version ? run : undefined
  return (
    <section
      className={`auto-flow-card ${automation.status === 'active' ? 'auto-flow-active' : ''}`}
      aria-label={t('flow')}
    >
      <div className="auto-canvas-heading">
        <span>{t('flow')}</span>
        <span>{t('version', { version: automation.version })}</span>
      </div>
      <div className="auto-zoom" role="group" aria-label={t('flow')}>
        <button
          type="button"
          aria-label={t('zoomOut')}
          onClick={() => setManualZoom(Math.max(0.2, zoom - 0.15))}
        >
          <Minus size={16} />
        </button>
        <output aria-label={t('zoom', { value: Math.round(zoom * 100) })}>
          {Math.round(zoom * 100)}%
        </output>
        <button
          type="button"
          aria-label={t('zoomIn')}
          onClick={() => setManualZoom(Math.min(1.6, zoom + 0.15))}
        >
          <Plus size={16} />
        </button>
        <button type="button" aria-label={t('fit')} onClick={() => setManualZoom(undefined)}>
          <Maximize size={16} />
        </button>
      </div>
      <div className="auto-flow-viewport" ref={viewport}>
        <div
          style={{ width: Math.max(available, layout.width * zoom), height: layout.height * zoom }}
        >
          <div
            className="auto-flow-stage"
            role="group"
            aria-label={t('flow')}
            style={{
              width: layout.width,
              height: layout.height,
              transform: `scale(${zoom})`,
              marginLeft: Math.max(0, (available - layout.width * zoom) / 2),
            }}
          >
            <svg
              className="auto-edges"
              width={layout.width}
              height={layout.height}
              aria-hidden="true"
            >
              <defs>
                <linearGradient
                  id={`${uid}-gradient`}
                  gradientUnits="userSpaceOnUse"
                  x1="0"
                  y1="0"
                  x2={layout.width}
                  y2={layout.height}
                >
                  <stop stopColor="var(--primary)" />
                  <stop offset="1" stopColor="var(--accent-2)" />
                </linearGradient>
                <marker
                  id={`${uid}-arrow`}
                  viewBox="0 0 10 10"
                  refX="8"
                  refY="5"
                  markerWidth="6"
                  markerHeight="6"
                  orient="auto-start-reverse"
                >
                  <path d="M 0 0 L 10 5 L 0 10 z" fill="var(--primary)" />
                </marker>
              </defs>
              {layout.edges.map((edge, index) => {
                const from = positions.get(edge.from)!
                const to = positions.get(edge.to)!
                const d = edgePath(from, to)
                const traversed = edgeTraversed(replay, edge.from, edge.to)
                return (
                  <g key={`${edge.from}-${edge.to}-${index}`}>
                    <path
                      className={`auto-edge ${traversed ? 'auto-edge-traversed' : ''}`}
                      d={d}
                      pathLength={1}
                      stroke={traversed ? `url(#${uid}-gradient)` : 'var(--border)'}
                      markerEnd={`url(#${uid}-arrow)`}
                    />
                    {!run && (
                      <path
                        className="auto-edge-flow"
                        d={d}
                        pathLength={100}
                        stroke={`url(#${uid}-gradient)`}
                      />
                    )}
                    {edge.label && (
                      <g
                        transform={`translate(${(from.x + to.x) / 2 + NODE_WIDTH / 2}, ${(from.y + NODE_HEIGHT + to.y) / 2})`}
                      >
                        <rect x="-21" y="-12" width="42" height="24" rx="12" />
                        <text textAnchor="middle" dominantBaseline="central">
                          {t(edge.label)}
                        </text>
                      </g>
                    )}
                  </g>
                )
              })}
            </svg>
            {layout.nodes.map((node, index) => {
              const step = steps.get(node.id)
              const type = step?.type ?? 'schedule'
              const status = step ? stepStatus(replay, node.id) : undefined
              const title = step?.title ?? t('schedule')
              const error = replay?.steps.find((item) => item.step_id === node.id)?.error
              return (
                <button
                  type="button"
                  key={node.id}
                  ref={(element) => {
                    if (element) buttons.current.set(node.id, element)
                    else buttons.current.delete(node.id)
                  }}
                  className={`auto-node auto-type-${type} ${status ? `auto-step-${status}` : ''} ${selected === node.id ? 'auto-node-selected' : ''}`}
                  data-step-id={node.id}
                  aria-pressed={selected === node.id}
                  title={error}
                  aria-label={t('nodeLabel', {
                    type: t(type),
                    title,
                    status: status ? ` · ${t(status)}` : '',
                  })}
                  style={
                    {
                      left: node.x,
                      top: node.y,
                      width: NODE_WIDTH,
                      height: NODE_HEIGHT,
                      '--auto-delay': `${index * 60}ms`,
                    } as CSSProperties
                  }
                  onClick={() => onSelect(node.id)}
                  onKeyDown={(event) => {
                    const directions: Record<string, [number, number]> = {
                      ArrowDown: [0, 1],
                      ArrowUp: [0, -1],
                      ArrowRight: [1, 0],
                      ArrowLeft: [-1, 0],
                    }
                    const direction = directions[event.key]
                    if (!direction) return
                    event.preventDefault()
                    const [dx, dy] = direction
                    const candidates = layout.nodes.filter((other) =>
                      dx ? (other.x - node.x) * dx > 0 : (other.y - node.y) * dy > 0,
                    )
                    candidates.sort(
                      (a, b) =>
                        Math.hypot(a.x - node.x, a.y - node.y) -
                        Math.hypot(b.x - node.x, b.y - node.y),
                    )
                    const next = candidates[0]
                    if (next) {
                      buttons.current.get(next.id)?.focus()
                      onSelect(next.id)
                    }
                  }}
                >
                  <span className="auto-node-top">
                    <span className="auto-type-icon">
                      <StepIcon type={type} />
                    </span>
                    <span className="auto-type-label">{t(type)}</span>
                    {step?.language && <span className="auto-language">{t(step.language)}</span>}
                    {status && (
                      <span
                        className={`auto-status-badge auto-badge-${status}`}
                        aria-label={t(status)}
                      >
                        {status === 'succeeded' ? (
                          <Check size={13} />
                        ) : status === 'failed' ? (
                          <X size={13} />
                        ) : (
                          <span />
                        )}
                      </span>
                    )}
                  </span>
                  <strong>{title}</strong>
                  <span className="auto-node-detail">{step ? stepDetail(step) : schedule}</span>
                </button>
              )
            })}
          </div>
        </div>
      </div>
    </section>
  )
}

import { useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import './i18n'

export function WorkspaceImage({
  url,
  path,
  interactive = false,
}: {
  url: string
  path: string
  interactive?: boolean
}) {
  const { t } = useTranslation('viewer')
  const [failed, setFailed] = useState(false)
  const [zoom, setZoom] = useState(1)
  const viewport = useRef<HTMLDivElement>(null)
  const drag = useRef<{ x: number; y: number; left: number; top: number } | null>(null)
  return failed ? (
    <div className="viewer-empty" role="alert">
      <h2>{t('openError')}</h2>
    </div>
  ) : (
    <div className="viewer-body">
      {interactive && (
        <div className="viewer-toolbar">
          <button onClick={() => setZoom((value) => Math.max(0.25, value - 0.25))}>
            {t('zoomOut')}
          </button>
          <button
            onClick={() => {
              setZoom(1)
              viewport.current?.scrollTo?.(0, 0)
            }}
          >
            {t('fit')}
          </button>
          <span>{Math.round(zoom * 100)}%</span>
          <button onClick={() => setZoom((value) => Math.min(4, value + 0.25))}>
            {t('zoomIn')}
          </button>
        </div>
      )}
      <div
        ref={viewport}
        className={interactive ? 'viewer-image-viewport' : undefined}
        tabIndex={interactive ? 0 : undefined}
        role={interactive ? 'region' : undefined}
        aria-label={interactive ? t('pan') : undefined}
        onPointerDown={
          interactive
            ? (event) => {
                if (event.button !== 0) return
                const node = event.currentTarget
                drag.current = {
                  x: event.clientX,
                  y: event.clientY,
                  left: node.scrollLeft,
                  top: node.scrollTop,
                }
                node.setPointerCapture(event.pointerId)
              }
            : undefined
        }
        onPointerMove={
          interactive
            ? (event) => {
                if (!drag.current) return
                event.currentTarget.scrollLeft =
                  drag.current.left - (event.clientX - drag.current.x)
                event.currentTarget.scrollTop = drag.current.top - (event.clientY - drag.current.y)
              }
            : undefined
        }
        onPointerUp={() => {
          drag.current = null
        }}
        onPointerCancel={() => {
          drag.current = null
        }}
        onLostPointerCapture={() => {
          drag.current = null
        }}
      >
        <img
          className="workspace-image"
          src={url}
          alt={path}
          draggable={false}
          style={
            interactive
              ? { width: `${zoom * 100}%`, maxWidth: 'none', maxHeight: 'none' }
              : undefined
          }
          onError={() => setFailed(true)}
        />
      </div>
    </div>
  )
}

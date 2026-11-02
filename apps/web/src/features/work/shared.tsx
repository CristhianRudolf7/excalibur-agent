import { WorkSkeleton, type WorkShape } from './WorkSkeleton'
import { useRef, type CSSProperties, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { useVirtualizer } from '@tanstack/react-virtual'
import { Inbox, RefreshCw } from 'lucide-react'
import './work.css'

export function DataState({
  loading,
  error,
  empty,
  retry,
  shape,
}: {
  shape?: WorkShape
  loading?: boolean
  error?: boolean
  empty?: boolean
  retry: () => void
}) {
  const { t } = useTranslation()
  if (loading) return <WorkSkeleton shape={shape} />
  if (error)
    return (
      <div className="work-state" role="alert">
        <RefreshCw />
        <p>{t('workOffline')}</p>
        <button onClick={retry}>{t('workRetry')}</button>
      </div>
    )
  if (empty)
    return (
      <div className="work-state">
        <Inbox />
        <h3>{t('workEmpty')}</h3>
        <p>{t('workEmptyHint')}</p>
      </div>
    )
  return null
}
export function VirtualRows<T>({
  rows,
  rowKey,
  render,
  height = 68,
  more,
}: {
  rows: T[]
  rowKey: (row: T) => string
  render: (row: T) => ReactNode
  height?: number
  more?: () => void
}) {
  const parent = useRef<HTMLDivElement>(null)
  // oxlint-disable-next-line react/incompatible-library -- Virtualizer owns scroll measurements.
  const virtual = useVirtualizer({
    count: rows.length,
    getScrollElement: () => parent.current,
    estimateSize: () => height,
    overscan: 6,
    getItemKey: (i) => rowKey(rows[i]!),
  })
  return (
    <div
      className="work-scroll"
      ref={parent}
      style={{ height: Math.min(rows.length * height, 560) }}
      onScroll={(e) => {
        const el = e.currentTarget
        if (el.scrollHeight - el.scrollTop - el.clientHeight < 200) more?.()
      }}
    >
      <div style={{ height: virtual.getTotalSize(), position: 'relative' }}>
        {virtual.getVirtualItems().map((item) => (
          <div
            className="work-virtual-row"
            key={item.key}
            style={
              {
                position: 'absolute',
                top: 0,
                left: 0,
                width: '100%',
                height: item.size,
                transform: `translateY(${item.start}px)`,
                '--work-delay': `${Math.min(item.index, 7) * 35}ms`,
              } as CSSProperties
            }
          >
            {render(rows[item.index]!)}
          </div>
        ))}
      </div>
    </div>
  )
}

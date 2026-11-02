import { Skeleton } from '@/components/shared'
import { useLoadingState } from '@/features/work/loading'

export type PanelShape = 'paper' | 'slides' | 'grid' | 'editor' | 'library' | 'canvas' | 'boards'
export function PanelSkeleton({
  label,
  shape = 'paper',
  delayed = false,
}: {
  label: string
  shape?: PanelShape
  delayed?: boolean
}) {
  const timing = useLoadingState(delayed)
  if (delayed && !timing.visible) return null
  return (
    <div
      className={`panel-placeholder panel-placeholder-${shape}`}
      role="status"
      aria-busy="true"
      aria-label={label}
    >
      {shape === 'boards' && (
        <div className="placeholder-board-heading">
          <Skeleton />
          <Skeleton />
          <Skeleton />
          <Skeleton />
        </div>
      )}
      {shape === 'slides' && (
        <div className="placeholder-thumbnails">
          {Array.from({ length: 3 }, (_, i) => (
            <Skeleton key={i} />
          ))}
        </div>
      )}
      {shape === 'canvas' && (
        <div className="placeholder-tools">
          {Array.from({ length: 7 }, (_, i) => (
            <Skeleton key={i} />
          ))}
        </div>
      )}
      <div className="placeholder-layout">
        {Array.from(
          { length: shape === 'library' || shape === 'boards' ? 3 : shape === 'grid' ? 40 : 10 },
          (_, i) => (
            <div className="placeholder-row" key={i}>
              {['editor', 'library', 'boards'].includes(shape) && (
                <Skeleton className="placeholder-marker" />
              )}
              <Skeleton className="placeholder-line" />
              {shape === 'library' && (
                <>
                  <Skeleton className="placeholder-detail" />
                  <Skeleton className="placeholder-action" />
                </>
              )}
            </div>
          ),
        )}
      </div>
    </div>
  )
}

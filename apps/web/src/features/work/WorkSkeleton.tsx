import { Skeleton } from '@/components/shared'
import { useTranslation } from 'react-i18next'

export type WorkShape =
  'tasks' | 'board' | 'reminders' | 'projects' | 'metrics' | 'detail' | 'files'

function CardLines() {
  return (
    <>
      <Skeleton className="work-loading-code" />
      <Skeleton className="work-loading-title" />
      <Skeleton className="work-loading-line" />
    </>
  )
}
export function WorkSkeleton({ shape = 'tasks', rows = 5 }: { shape?: WorkShape; rows?: number }) {
  const { t } = useTranslation()
  const cards = shape === 'board' || shape === 'projects' || shape === 'metrics'
  return (
    <div
      className={`work-loading work-loading-${shape}`}
      role="status"
      aria-busy="true"
      aria-label={t('workLoading')}
    >
      {Array.from(
        {
          length: cards
            ? shape === 'metrics'
              ? 4
              : shape === 'board'
                ? 6
                : 3
            : shape === 'reminders'
              ? 3
              : rows,
        },
        (_, index) => (
          <div className="work-loading-item" key={index}>
            {shape === 'board' ? (
              <>
                <div className="work-loading-column-title">
                  <Skeleton className="work-loading-icon" />
                  <Skeleton className="work-loading-title" />
                </div>
                {[0, 1].map((card) => (
                  <div className="work-loading-card" key={card}>
                    <CardLines />
                    <Skeleton className="work-loading-progress" />
                  </div>
                ))}
              </>
            ) : shape === 'metrics' ? (
              <>
                <div className="work-loading-column-title">
                  <Skeleton className="work-loading-icon" />
                  <Skeleton className="work-loading-code" />
                </div>
                <Skeleton className="work-loading-number" />
                <Skeleton className="work-loading-line" />
              </>
            ) : shape === 'reminders' ? (
              <>
                <Skeleton className="work-loading-code" />
                <div className="work-loading-card">
                  <Skeleton className="work-loading-title" />
                  <Skeleton className="work-loading-line" />
                  <div className="work-loading-actions">
                    {[0, 1, 2].map((action) => (
                      <Skeleton key={action} />
                    ))}
                  </div>
                </div>
              </>
            ) : shape === 'projects' ? (
              <>
                <Skeleton className="work-loading-icon" />
                <CardLines />
                <Skeleton className="work-loading-progress" />
              </>
            ) : (
              <>
                <Skeleton className="work-loading-icon" />
                <div className="work-loading-copy">
                  <CardLines />
                </div>
                <Skeleton className="work-loading-progress" />
              </>
            )}
          </div>
        ),
      )}
    </div>
  )
}
export function PendingSpinner() {
  return <span className="work-pending-spinner" aria-hidden="true" />
}

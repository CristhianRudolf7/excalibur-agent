import { useTranslation } from 'react-i18next'
import { LoadingStatus, Skeleton } from '@/components/shared'
export function FieldsSkeleton({ editor = false }: { editor?: boolean }) {
  const { t } = useTranslation()
  return (
    <LoadingStatus
      label={t(editor ? 'loadingEditor' : 'settingsLoading')}
      className="settings-fields-skeleton"
    >
      {!editor && (
        <>
          <Skeleton className="sk-title" />
          <Skeleton className="sk-field" />
          <Skeleton className="sk-title" />
          <Skeleton className="sk-field" />
        </>
      )}
      {editor && (
        <div className="sk-editor">
          {Array.from({ length: 7 }, (_, i) => (
            <div key={i}>
              <Skeleton className="sk-gutter" />
              <Skeleton className={`sk-line sk-line-${i % 3}`} />
            </div>
          ))}
        </div>
      )}
    </LoadingStatus>
  )
}
export function ChartSkeleton() {
  const { t } = useTranslation()
  return (
    <LoadingStatus label={t('settingsLoading')} className="sk-chart">
      {[45, 70, 55, 85, 65, 95, 75, 60, 80, 50, 90, 65].map((height, i) => (
        <div key={i} style={{ height: `${height}%` }}>
          <Skeleton />
        </div>
      ))}
    </LoadingStatus>
  )
}
export function UsageSkeleton() {
  const { t } = useTranslation()
  return (
    <LoadingStatus label={t('settingsLoading')} className="sk-usage">
      <div className="usage-kpis">
        {Array.from({ length: 5 }, (_, i) => (
          <div className="settings-card" key={i}>
            <Skeleton className="sk-title" />
            <Skeleton className="sk-number" />
            <Skeleton />
          </div>
        ))}
      </div>
      <div className="settings-card">
        <Skeleton className="sk-title" />
        <ChartSkeleton />
      </div>
      <div className="usage-breakdowns">
        {[0, 1].map((i) => (
          <div className="settings-card" key={i}>
            <Skeleton className="sk-title" />
            <ChartSkeleton />
          </div>
        ))}
      </div>
    </LoadingStatus>
  )
}
export function SkillsSkeleton() {
  const { t } = useTranslation()
  return (
    <LoadingStatus label={t('settingsLoading')} className="sk-skills">
      <div className="sk-chips">
        {[0, 1, 2, 3].map((i) => (
          <Skeleton key={i} />
        ))}
      </div>
      <div className="sk-card-grid">
        {[0, 1, 2, 3].map((i) => (
          <div className="settings-card" key={i}>
            <Skeleton className="sk-title" />
            <Skeleton />
            <Skeleton className="sk-line-1" />
            <Skeleton className="sk-title" />
          </div>
        ))}
      </div>
    </LoadingStatus>
  )
}
export function SettingsSkeleton() {
  const { t } = useTranslation()
  return (
    <LoadingStatus label={t('settingsLoading')} className="settings-page sk-settings-page">
      <div className="sk-settings-heading">
        <Skeleton className="sk-title" />
        <Skeleton className="sk-number" />
        <Skeleton className="sk-line-1" />
      </div>
      <div className="settings-layout">
        <div className="sk-settings-nav">
          {[0, 1, 2, 3].map((i) => (
            <Skeleton className="sk-field" key={i} />
          ))}
        </div>
        <div className="settings-card settings-fields-skeleton">
          <Skeleton className="sk-title" />
          <Skeleton className="sk-field" />
          <Skeleton className="sk-title" />
          <Skeleton className="sk-field" />
        </div>
      </div>
    </LoadingStatus>
  )
}

import { useTranslation } from 'react-i18next'
import { LoadingStatus, Skeleton } from '@/components/shared'
export function LabSkeleton() {
  const { t } = useTranslation()
  return (
    <LoadingStatus label={t('lab_title')} className="sk-lab">
      <div className="sk-lab-toolbar">
        <Skeleton className="sk-avatar" />
        <Skeleton className="sk-title" />
      </div>
      <Skeleton className="sk-lab-canvas" />
      <div className="sk-chips">
        <Skeleton />
        <Skeleton />
        <Skeleton />
      </div>
    </LoadingStatus>
  )
}

export function AgentHeaderSkeleton() {
  const { t } = useTranslation()
  return (
    <LoadingStatus label={t('chatReconnecting')} className="sk-agent-header">
      <Skeleton className="sk-avatar" />
      <div>
        <Skeleton />
        <Skeleton className="sk-line-1" />
      </div>
    </LoadingStatus>
  )
}

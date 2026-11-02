import { useTranslation } from 'react-i18next'
import { LoadingStatus, Skeleton } from '@/components/shared'
export function ChatHistorySkeleton({ compact = false }: { compact?: boolean }) {
  const { t } = useTranslation()
  return (
    <LoadingStatus label={t('chatLoadingMessages')} className="sk-history">
      {Array.from({ length: compact ? 2 : 4 }, (_, i) => (
        <div className={`sk-message sk-message-${i % 2}`} key={i}>
          {i % 2 === 0 && <Skeleton className="sk-avatar" />}
          <div className="sk-bubble">
            <Skeleton className="sk-title" />
            <Skeleton />
            <Skeleton className="sk-line-1" />
            {i % 2 === 0 && <Skeleton className="sk-line-2" />}
          </div>
        </div>
      ))}
    </LoadingStatus>
  )
}

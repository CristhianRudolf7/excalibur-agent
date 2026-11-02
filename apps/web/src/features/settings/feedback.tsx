import { useTranslation } from 'react-i18next'
import type { ReactNode } from 'react'
import { useLoading } from '@/components/shared'
import { FieldsSkeleton } from './Skeletons'
export function Feedback({
  query,
  skeleton,
  loading: suppliedLoading,
}: {
  loading?: ReturnType<typeof useLoading>
  skeleton?: ReactNode
  query: { isPending: boolean; isError: boolean; refetch: () => unknown }
}) {
  const { t } = useTranslation()
  const internalLoading = useLoading(query.isPending)
  const loading = suppliedLoading ?? internalLoading
  if (loading.busy) return loading.visible ? (skeleton ?? <FieldsSkeleton />) : null
  if (query.isError)
    return (
      <div className="settings-feedback" role="alert">
        <p>{t('settingsOffline')}</p>
        <button onClick={() => query.refetch()}>{t('settingsRetry')}</button>
      </div>
    )
  return null
}

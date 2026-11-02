import { LoadingStatus, Skeleton, useLoading } from '@/components/shared'
import type { ComponentProps } from 'react'
import { useTranslation } from 'react-i18next'
import { useProfile } from './api'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import { initials } from './initials'

export function ProfileSkeleton() {
  const { t } = useTranslation()
  return (
    <LoadingStatus label={t('profileLoading')} className="profile-details">
      <Skeleton className="sk-avatar" />
      <div className="sk-profile-lines">
        <Skeleton className="sk-title" />
        <Skeleton />
      </div>
    </LoadingStatus>
  )
}
export function ProfileState() {
  const { t } = useTranslation()
  const profile = useProfile()
  const loading = useLoading(profile.isPending)
  if (loading.busy) return loading.visible ? <ProfileSkeleton /> : null
  if (profile.isError || !profile.data)
    return (
      <div className="profile-error" role="alert">
        <p>{t('profileOffline')}</p>
        <Button onClick={() => void profile.refetch()}>{t('profileRetry')}</Button>
      </div>
    )
  return (
    <div className="profile-details">
      <span className="user-avatar" aria-hidden="true">
        {initials(profile.data.name)}
      </span>
      <dl>
        <dt>{t('profileName')}</dt>
        <dd>{profile.data.name}</dd>
      </dl>
    </div>
  )
}
export function ProfileDialog({ onClose }: { onClose: () => void }) {
  return (
    <Dialog title="profileTitle" onClose={onClose}>
      <ProfileState />
    </Dialog>
  )
}
export function ProfileButton({
  avatarOnly = false,
  ...props
}: ComponentProps<'button'> & { avatarOnly?: boolean }) {
  const { t } = useTranslation()
  const profile = useProfile()
  const loading = useLoading(profile.isPending)
  return (
    <button type="button" className="profile-button" aria-label={t('profileTitle')} {...props}>
      <span className="user-avatar">
        {profile.data ? initials(profile.data.name) : <span aria-hidden="true">—</span>}
      </span>
      {!avatarOnly &&
        (loading.busy ? (
          <span role="status" aria-busy="true" aria-label={t('profileLoading')}>
            {loading.visible && <Skeleton className="profile-skeleton" />}
          </span>
        ) : (
          <span className="profile-user-name">{profile.data?.name ?? t('profilePlaceholder')}</span>
        ))}
    </button>
  )
}

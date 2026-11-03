import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { LockKeyhole, ShieldCheck } from 'lucide-react'
import { rpc } from '@/lib/rpc/client'
import type { VaultMetadata } from '@/lib/rpc/credentials'
import { LoadingStatus, Skeleton } from '@/components/shared'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { UnlockVault } from '@/features/credentials/UnlockVault'
import { vaultLocked } from '@/features/credentials/secure'

const key = ['vault-metadata'] as const
function siteHosts(entry: VaultMetadata): string[] {
  const hosts = entry.domains?.length
    ? entry.domains
    : [entry.metadata?.host ?? entry.metadata?.domain ?? entry.metadata?.site ?? '']
  if (hosts.some(Boolean)) return hosts.filter(Boolean)
  try {
    return entry.metadata?.url ? [new URL(entry.metadata.url).host] : []
  } catch {
    return []
  }
}
export function Passwords() {
  const { t, i18n } = useTranslation()
  const client = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(false)
  const [observedLocked, setObservedLocked] = useState<boolean>()
  const [unlocking, setUnlocking] = useState(false)
  const [deleting, setDeleting] = useState<VaultMetadata | null>(null)
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => rpc.call('vault.list_metadata', {}, { signal }),
    retry: false,
  })
  const locked = query.data?.locked ?? observedLocked
  const entries = (query.data?.entries ?? []).filter(
    (entry) =>
      !entry.expires_at && (entry.type === 'website_password' || siteHosts(entry).length > 0),
  )
  const recordLockState = (next: boolean) => {
    setObservedLocked(next)
    client.setQueryData<typeof query.data>(key, (data) => (data ? { ...data, locked: next } : data))
  }
  const refresh = () => client.invalidateQueries({ queryKey: key })
  const date = (value: string) => new Date(value).toLocaleString(i18n.language)
  return (
    <div className="grid gap-5">
      <div className="settings-card grid gap-4">
        <p role="status" className="flex items-center gap-2">
          {locked === false ? (
            <ShieldCheck aria-hidden="true" size={18} />
          ) : (
            <LockKeyhole aria-hidden="true" size={18} />
          )}
          {t(
            locked === true
              ? 'passwordsLocked'
              : locked === false
                ? 'passwordsUnlocked'
                : 'passwordsUnknown',
          )}
        </p>
        <div className="flex flex-wrap gap-2">
          {locked !== false && (
            <Button disabled={busy} onClick={() => setUnlocking(!unlocking)}>
              {t('credentialUnlock')}
            </Button>
          )}
          {locked !== true && (
            <Button
              disabled={busy}
              onClick={async () => {
                if (busy) return
                setBusy(true)
                setError(false)
                try {
                  const result = await rpc.call('vault.lock', {})
                  if (!result.locked) throw new Error('Lock failed')
                  recordLockState(true)
                  setUnlocking(false)
                  await refresh()
                } catch {
                  setError(true)
                } finally {
                  setBusy(false)
                }
              }}
            >
              {t('passwordsLock')}
            </Button>
          )}
        </div>
        {unlocking && (
          <UnlockVault
            onUnlocked={async () => {
              recordLockState(false)
              setUnlocking(false)
              await refresh()
            }}
          />
        )}
      </div>
      {(error || query.isError) && (
        <div role="alert" className="settings-feedback">
          <p>{t('credentialError')}</p>
          <Button onClick={() => void query.refetch()}>{t('credentialRetry')}</Button>
        </div>
      )}
      {query.isPending && (
        <LoadingStatus
          label={t('passwordsLoading')}
          className="settings-card settings-passwords-skeleton"
        >
          <Skeleton className="sk-title" />
          <Skeleton className="sk-field" />
        </LoadingStatus>
      )}
      {query.isSuccess && entries.length === 0 && <p>{t('passwordsEmpty')}</p>}
      <ul className="grid gap-3">
        {entries.map((entry) => (
          <li
            key={entry.id}
            className="settings-card flex min-w-0 flex-wrap items-start justify-between gap-4"
          >
            <div className="grid min-w-0 gap-1">
              <strong className="break-all">{entry.name}</strong>
              {siteHosts(entry).map((host) => (
                <span key={host} className="break-all text-muted-foreground">
                  {host}
                </span>
              ))}
              <small>{t('passwordsCreated', { date: date(entry.created_at) })}</small>
              <small>{t('passwordsUpdated', { date: date(entry.updated_at) })}</small>
            </div>
            <Button
              disabled={busy}
              onClick={() => {
                setError(false)
                setDeleting(entry)
              }}
            >
              {t('passwordsDelete', { name: entry.name })}
            </Button>
          </li>
        ))}
      </ul>
      {deleting && (
        <Dialog
          title="passwordsTitle"
          onClose={() => {
            if (!busy) setDeleting(null)
          }}
        >
          <p>{t('passwordsConfirm', { name: deleting.name })}</p>
          <div className="settings-actions">
            <Button disabled={busy} onClick={() => setDeleting(null)}>
              {t('credentialCancel')}
            </Button>
            <Button
              variant="destructive"
              disabled={busy}
              onClick={async () => {
                if (busy) return
                setBusy(true)
                setError(false)
                try {
                  await rpc.call('vault.delete', { name: deleting.name, confirmed: true })
                  setDeleting(null)
                  await refresh()
                } catch (cause) {
                  if (vaultLocked(cause)) {
                    recordLockState(true)
                    setUnlocking(true)
                  }
                  setError(true)
                  setDeleting(null)
                } finally {
                  setBusy(false)
                }
              }}
            >
              {t('passwordsConfirmDelete')}
            </Button>
          </div>
        </Dialog>
      )}
    </div>
  )
}

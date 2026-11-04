import { useEffect, useRef, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { LockKeyhole, ShieldCheck } from 'lucide-react'
import { rpc } from '@/lib/rpc/client'
import { loginAddress, loginCards, siteHosts } from '@/lib/rpc/credentials'
import type { LoginCard } from '@/lib/rpc/credentials'
import { LoadingStatus, Skeleton } from '@/components/shared'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { UnlockVault } from '@/features/credentials/UnlockVault'
import { SecretInput } from '@/features/credentials/SecretInput'
import { vaultLocked } from '@/features/credentials/secure'

const key = ['vault-metadata'] as const
export function Passwords() {
  const { t, i18n } = useTranslation()
  const client = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(false)
  const [observedLocked, setObservedLocked] = useState<boolean>()
  const [unlocking, setUnlocking] = useState(false)
  const [deleting, setDeleting] = useState<LoginCard | null>(null)
  const [adding, setAdding] = useState(false)
  const [label, setLabel] = useState('')
  const [address, setAddress] = useState('')
  const [loginHost, setLoginHost] = useState('')
  const [invalidAddress, setInvalidAddress] = useState(false)
  const form = useRef<HTMLFormElement>(null)
  useEffect(() => {
    const node = form.current
    return () => {
      for (const input of node?.querySelectorAll<HTMLInputElement>('[data-login-secret]') ?? [])
        input.value = ''
    }
  }, [adding])
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
  const cards = loginCards(entries)
  const recordLockState = (next: boolean) => {
    setObservedLocked(next)
    client.setQueryData<typeof query.data>(key, (data) => (data ? { ...data, locked: next } : data))
  }
  const refresh = () => client.invalidateQueries({ queryKey: key })
  const date = (value: string) => new Date(value).toLocaleString(i18n.language)
  return (
    <div className="grid min-w-0 gap-5">
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
          <Button
            disabled={busy}
            onClick={() => {
              setError(false)
              setInvalidAddress(false)
              setAdding(true)
            }}
          >
            {t('passwordsAdd')}
          </Button>
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
        {unlocking && !adding && (
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
      <ul className="grid min-w-0 gap-3">
        {cards.map((card) => (
          <li
            key={card.id}
            className="settings-card flex min-w-0 flex-wrap items-start justify-between gap-4"
          >
            <div className="grid min-w-0 gap-1">
              <strong className="break-all">{card.label}</strong>
              {card.host.map((host) => (
                <span key={host} className="break-all text-muted-foreground">
                  {host}
                </span>
              ))}
              {card.username && <small>{t('passwordsUsernamePresent')}</small>}
              {card.email && <small>{t('passwordsEmailPresent')}</small>}
              <small>
                {t('passwordsCreated', { date: date(card.entries[0]?.created_at ?? '') })}
              </small>
              <small>
                {t('passwordsUpdated', { date: date(card.entries[0]?.updated_at ?? '') })}
              </small>
            </div>
            <Button
              className="max-w-full text-left break-all whitespace-normal"
              disabled={busy}
              onClick={() => {
                setError(false)
                setDeleting(card)
              }}
            >
              {t('passwordsDelete', { name: card.label })}
            </Button>
          </li>
        ))}
      </ul>
      {adding && (
        <Dialog
          title="passwordsAdd"
          className="max-w-[calc(100vw-2rem)] min-w-0"
          footer={false}
          onClose={() => {
            if (!busy) setAdding(false)
          }}
        >
          <div className="grid min-w-0 gap-4">
            {locked === true && (
              <UnlockVault
                onUnlocked={async () => {
                  recordLockState(false)
                  setUnlocking(false)
                  await refresh()
                }}
              />
            )}
            <form
              ref={form}
              autoComplete="off"
              className="grid min-w-0 gap-3"
              onSubmit={async (event) => {
                event.preventDefault()
                if (busy) return
                const username = form.current?.elements.namedItem(
                  'saved-login-username',
                ) as HTMLInputElement | null
                const password = form.current?.elements.namedItem(
                  'saved-login-password',
                ) as HTMLInputElement | null
                const values = { username: username?.value ?? '', password: password?.value ?? '' }
                if (username) username.value = ''
                if (password) password.value = ''
                setBusy(true)
                setError(false)
                setInvalidAddress(false)
                try {
                  let target
                  try {
                    target = loginAddress(address, loginHost)
                  } catch {
                    setInvalidAddress(true)
                    return
                  }
                  const result = await rpc.call('credentials.save_login', {
                    ...target,
                    label: label.trim(),
                    values,
                  })
                  if (!result.ok) throw new Error('Save failed')
                  setAdding(false)
                  setLabel('')
                  setAddress('')
                  setLoginHost('')
                  await refresh()
                } catch (cause) {
                  if (vaultLocked(cause)) {
                    recordLockState(true)
                    setUnlocking(true)
                  }
                  setError(true)
                } finally {
                  values.username = ''
                  values.password = ''
                  setBusy(false)
                }
              }}
            >
              <label className="grid min-w-0 gap-2">
                {t('passwordsLabel')}
                <input
                  className="min-w-0 rounded-md border border-border bg-background px-3 py-2"
                  value={label}
                  onChange={(event) => setLabel(event.target.value)}
                  disabled={busy}
                />
              </label>
              <label className="grid min-w-0 gap-2">
                {t('passwordsAddress')}
                <input
                  className="min-w-0 rounded-md border border-border bg-background px-3 py-2"
                  value={address}
                  onChange={(event) => setAddress(event.target.value)}
                  required
                  disabled={busy}
                />
              </label>
              <label className="grid min-w-0 gap-2">
                {t('passwordsLoginHost')}
                <input
                  className="min-w-0 rounded-md border border-border bg-background px-3 py-2"
                  value={loginHost}
                  onChange={(event) => setLoginHost(event.target.value)}
                  aria-describedby="saved-login-host-hint"
                  disabled={busy}
                />
              </label>
              <p id="saved-login-host-hint" className="text-sm text-muted-foreground">
                {t('passwordsLoginHostHint')}
              </p>
              {locked !== true && (
                <>
                  <label className="grid min-w-0 gap-2">
                    {t('credentialUsername')}
                    <input
                      className="min-w-0 rounded-md border border-border bg-background px-3 py-2"
                      name="saved-login-username"
                      data-login-secret
                      autoComplete="off"
                      required
                      disabled={busy}
                    />
                  </label>
                  <SecretInput
                    label={t('credentialPassword')}
                    name="saved-login-password"
                    data-login-secret
                    autoComplete="new-password"
                    required
                    disabled={busy}
                  />
                </>
              )}
              {invalidAddress && <p role="alert">{t('passwordsInvalidAddress')}</p>}
              {error && <p role="alert">{t('credentialError')}</p>}
              <div className="settings-actions">
                <Button disabled={busy} onClick={() => setAdding(false)}>
                  {t('credentialCancel')}
                </Button>
                <Button type="submit" variant="primary" disabled={busy || locked === true}>
                  {t('passwordsSave')}
                </Button>
              </div>
            </form>
          </div>
        </Dialog>
      )}
      {deleting && (
        <Dialog
          title="passwordsTitle"
          onClose={() => {
            if (!busy) setDeleting(null)
          }}
        >
          <p>{t('passwordsConfirm', { name: deleting.label })}</p>
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
                  for (const entry of deleting.entries)
                    await rpc.call('vault.delete', { name: entry.name, confirmed: true })
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

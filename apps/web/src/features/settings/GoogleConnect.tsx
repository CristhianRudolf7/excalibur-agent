import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, CheckCircle2, LoaderCircle } from 'lucide-react'
import { rpc } from '@/lib/rpc/client'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { googleQueryKey, googleStateKey, permissionName, useGoogleNotice } from './google-flow'
import './google-connect.css'

export function GoogleLogo() {
  return (
    <svg className="google-logo" viewBox="0 0 24 24" aria-hidden="true">
      <path
        fill="currentColor"
        d="M21.6 12.23c0-.71-.06-1.39-.18-2.05H12v3.88h5.38a4.6 4.6 0 0 1-2 3.02v2.51h3.24c1.9-1.75 2.98-4.33 2.98-7.36ZM12 22c2.7 0 4.96-.9 6.62-2.41l-3.24-2.51c-.9.6-2.04.97-3.38.97-2.6 0-4.8-1.76-5.59-4.12H3.07v2.59A10 10 0 0 0 12 22ZM6.41 13.93A6 6 0 0 1 6.1 12c0-.67.11-1.32.31-1.93V7.48H3.07A10 10 0 0 0 2 12c0 1.61.38 3.14 1.07 4.52l3.34-2.59ZM12 5.95c1.47 0 2.79.51 3.82 1.51l2.87-2.87A9.6 9.6 0 0 0 12 2a10 10 0 0 0-8.93 5.48l3.34 2.59C7.2 7.71 9.4 5.95 12 5.95Z"
      />
    </svg>
  )
}

export function GoogleConnect() {
  const { t, i18n } = useTranslation()
  const client = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(false)
  const [confirm, setConfirm] = useState(false)
  const [now, setNow] = useState(Date.now)
  const notice = useGoogleNotice((s) => s.notice)
  const setNotice = useGoogleNotice((s) => s.setNotice)
  const query = useQuery({
    queryKey: googleQueryKey,
    queryFn: ({ signal }) => rpc.call('google.status', {}, { signal }),
    retry: false,
    refetchInterval: 60_000,
  })
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 60_000)
    return () => window.clearInterval(timer)
  }, [])
  const status = query.data
  const expiry = status?.expires_at ? new Date(status.expires_at).getTime() : undefined
  const remaining =
    expiry === undefined ? undefined : Math.max(0, Math.ceil((expiry - now) / 3_600_000))
  const expiring = expiry !== undefined && expiry - now < 2 * 86_400_000
  const missing = status?.missing_scopes ?? []
  const reconnect =
    status?.connected ||
    missing.length > 0 ||
    ['invalid_grant', 'refresh_expired'].includes(status?.reason ?? '')
  const connect = async () => {
    if (busy) return
    setBusy(true)
    setError(false)
    try {
      const { auth_url } = await rpc.call('google.connect.start', {})
      const url = new URL(auth_url)
      if (!['https:', 'http:'].includes(url.protocol)) throw new Error('Invalid authorization URL')
      try {
        sessionStorage.setItem(googleStateKey, url.searchParams.get('state') ?? '')
      } catch {
        /* Server validates state even when browser storage is unavailable. */
      }
      window.location.assign(url.href)
    } catch {
      setError(true)
      setBusy(false)
    }
  }
  return (
    <div className="settings-card google-connect">
      <header className="google-heading">
        <GoogleLogo />
        <h3>{t('googleTitle')}</h3>
      </header>
      {notice && (
        <div className="google-notice" role={notice === 'success' ? 'status' : 'alert'}>
          <p>
            {t(
              {
                success: 'googleSuccess',
                error: 'googleCallbackError',
                denied: 'googleDenied',
                missing: 'googleMissingNotice',
              }[notice],
            )}
          </p>
          <Button onClick={() => setNotice(null)}>{t('close')}</Button>
        </div>
      )}
      {query.isPending && (
        <p role="status">
          <LoaderCircle className="google-spinner" aria-hidden="true" />
          {t('settingsLoading')}
        </p>
      )}
      {(query.isError || error) && (
        <p role="alert">
          {t('googleError')}{' '}
          <Button onClick={() => void query.refetch()}>{t('settingsRetry')}</Button>
        </p>
      )}
      {status && (
        <>
          <p role="status" className="google-state">
            {status.connected && <CheckCircle2 size={18} aria-hidden="true" />}
            {t(
              !status.configured
                ? 'googleNotConfigured'
                : status.connected
                  ? 'googleConnected'
                  : 'googleDisconnected',
              { email: status.email ?? t('googleAccount') },
            )}
          </p>
          {!status.configured && (
            <p>
              {t('googleSetup')} <code>~/.config/excalibur/credentials/google-oauth.json</code>
            </p>
          )}
          {status.reason === 'vault_locked' && <p role="alert">{t('googleVaultLocked')}</p>}
          {['invalid_grant', 'refresh_expired'].includes(status.reason ?? '') && (
            <p role="alert">{t('googleReconnectReason')}</p>
          )}
          {!!status.scopes?.length && (
            <ul className="google-permissions" aria-label={t('googlePermissions')}>
              {[...new Set(status.scopes.map(permissionName))].map((name) => (
                <li key={name}>{t(name)}</li>
              ))}
            </ul>
          )}
          {missing.length > 0 && (
            <p role="alert">
              {t('googleMissing', {
                permissions: [...new Set(missing.map(permissionName))]
                  .map((name) => t(name))
                  .join(', '),
              })}
            </p>
          )}
          {remaining !== undefined && (
            <p className={expiring ? 'google-warning' : ''} role={expiring ? 'alert' : undefined}>
              {expiring && <AlertTriangle size={16} aria-hidden="true" />}
              {t(remaining === 0 ? 'googleExpired' : 'googleExpiry', { count: remaining })}
              {' · '}
              {new Date(expiry!).toLocaleString(i18n.language)}
            </p>
          )}
          {expiring && remaining !== 0 && <p>{t('googleExpiryWarning')}</p>}
          {status.warning && (
            <p className="google-warning" role="alert">
              {t(
                status.warning === 'file_storage'
                  ? 'googleFileStorage'
                  : status.warning === 'vault_locked'
                    ? 'googleVaultLocked'
                    : 'googleStorageError',
              )}
            </p>
          )}
          <div className="google-actions">
            <Button disabled={busy || !status.configured} onClick={() => void connect()}>
              {t(reconnect ? 'googleReconnect' : 'googleConnect')}
            </Button>
            {status.connected && (
              <Button disabled={busy} onClick={() => setConfirm(true)}>
                {t('googleDisconnect')}
              </Button>
            )}
          </div>
        </>
      )}
      {confirm && (
        <Dialog title="googleDisconnectTitle" onClose={() => setConfirm(false)} footer={false}>
          <p>{t('googleDisconnectNote')}</p>
          <div className="google-actions">
            <Button
              disabled={busy}
              onClick={async () => {
                setBusy(true)
                setError(false)
                try {
                  const next = await rpc.call('google.disconnect', {})
                  client.setQueryData(googleQueryKey, next)
                  setNotice(null)
                  setConfirm(false)
                  await client.invalidateQueries({ queryKey: googleQueryKey })
                } catch {
                  setError(true)
                } finally {
                  setBusy(false)
                }
              }}
            >
              {t('googleDisconnect')}
            </Button>
            <Button disabled={busy} onClick={() => setConfirm(false)}>
              {t('googleCancel')}
            </Button>
          </div>
        </Dialog>
      )}
    </div>
  )
}

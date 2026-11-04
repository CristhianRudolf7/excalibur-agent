import { useEffect, useId, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { rpc } from '@/lib/rpc/client'
import { Button } from '@/components/ui/button'
import './codex-connect.css'

const queryKey = ['codex', 'status'] as const
const loginLifetime = 10 * 60_000

export function CodexConnect() {
  const { t } = useTranslation()
  const client = useQueryClient()
  const inputID = useId()
  const helpID = useId()
  const [busy, setBusy] = useState(false)
  const [authURL, setAuthURL] = useState('')
  const [paste, setPaste] = useState('')
  const [notice, setNotice] = useState<'error' | 'expired' | ''>('')
  const query = useQuery({
    queryKey,
    queryFn: ({ signal }) => rpc.call('codex.status', {}, { signal }),
    retry: false,
    refetchInterval: authURL ? 2_000 : 60_000,
  })
  const status = query.data
  const [observedStatus, setObservedStatus] = useState(status)
  // Discard temporary credentials when refreshed server metadata reports completion.
  if (status !== observedStatus) {
    setObservedStatus(status)
    if (status?.connected) {
      setAuthURL('')
      setPaste('')
    }
  }
  useEffect(() => {
    if (!authURL) return
    const timer = window.setTimeout(() => {
      setAuthURL('')
      setPaste('')
      setNotice('expired')
    }, loginLifetime)
    return () => window.clearTimeout(timer)
  }, [authURL])

  const connect = async () => {
    setBusy(true)
    setNotice('')
    setAuthURL('')
    setPaste('')
    try {
      const { auth_url } = await rpc.call('codex.login.start', {})
      const url = new URL(auth_url)
      if (url.protocol !== 'https:' || !url.searchParams.get('state'))
        throw new Error('Invalid authorization URL')
      setAuthURL(url.href)
    } catch {
      setNotice('error')
    } finally {
      setBusy(false)
    }
  }
  const complete = async () => {
    setBusy(true)
    setNotice('')
    try {
      const state = new URL(authURL).searchParams.get('state') ?? ''
      const next = await rpc.call(
        'codex.login.complete',
        { code: paste.trim(), state },
        { timeoutMs: 30_000 },
      )
      client.setQueryData(queryKey, next)
    } catch {
      setNotice('error')
    } finally {
      setPaste('')
      setAuthURL('')
      setBusy(false)
    }
  }
  const disconnect = async () => {
    setBusy(true)
    setNotice('')
    try {
      const next = await rpc.call('codex.logout', {})
      client.setQueryData(queryKey, next)
      setAuthURL('')
      setPaste('')
    } catch {
      setNotice('error')
    } finally {
      setBusy(false)
    }
  }
  const sourceKey = {
    excalibur: 'codexSourceOwn',
    'codex-cli': 'codexSourceCLI',
    env: 'codexSourceEnv',
    '': 'codexSourceUnknown',
  } as const

  return (
    <section className="settings-card codex-connect" aria-label={t('codexTitle')}>
      <h3>{t('codexTitle')}</h3>
      {query.isPending && <p role="status">{t('settingsLoading')}</p>}
      {(query.isError || notice === 'error') && (
        <p role="alert">
          {t('codexError')}{' '}
          <Button disabled={busy} onClick={() => void query.refetch()}>
            {t('settingsRetry')}
          </Button>
        </p>
      )}
      {notice === 'expired' && <p role="alert">{t('codexExpired')}</p>}
      {status && (
        <>
          <p role="status">
            {t(status.connected ? 'codexConnected' : 'codexDisconnected', {
              email: status.account_email || t('codexAccount'),
            })}
          </p>
          {status.connected && (
            <>
              <p>{t('codexPlan', { plan: status.plan_type || t('codexPlanUnknown') })}</p>
              <p>
                {t('codexSource', { source: t(sourceKey[status.source] ?? 'codexSourceUnknown') })}
              </p>
              {['codex-cli', 'env'].includes(status.source) && <p>{t('codexExternal')}</p>}
            </>
          )}
        </>
      )}
      <p>{t('codexLimits')}</p>
      <div className="codex-actions">
        <Button disabled={busy} onClick={() => void connect()}>
          {t('codexConnect')}
        </Button>
        {(status?.connected || authURL) && (
          <Button disabled={busy} onClick={() => void disconnect()}>
            {t('codexDisconnect')}
          </Button>
        )}
      </div>
      {authURL && (
        <form
          className="codex-paste"
          onSubmit={(event) => {
            event.preventDefault()
            if (!busy && paste.trim()) void complete()
          }}
        >
          <a href={authURL} target="_blank" rel="noopener noreferrer">
            {t('codexOpen')}
          </a>
          <p id={helpID}>{t('codexRemote')}</p>
          <label htmlFor={inputID}>{t('codexPaste')}</label>
          <input
            id={inputID}
            aria-describedby={helpID}
            value={paste}
            onChange={(event) => setPaste(event.target.value)}
            maxLength={8192}
            autoComplete="off"
            spellCheck={false}
            disabled={busy}
          />
          <Button type="submit" disabled={busy || !paste.trim()}>
            {t('codexComplete')}
          </Button>
          <p role="status">{t('codexWaiting')}</p>
        </form>
      )}
    </section>
  )
}

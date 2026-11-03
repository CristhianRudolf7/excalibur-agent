import { useEffect, useRef, useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { LoaderCircle } from 'lucide-react'
import { rpc } from '@/lib/rpc/client'
import { googleQueryKey, googleStateKey, useGoogleNotice, type GoogleNotice } from './google-flow'
import './google-connect.css'

export function GoogleCallback() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const client = useQueryClient()
  const setNotice = useGoogleNotice((s) => s.setNotice)
  const [params] = useState(() => new URLSearchParams(window.location.search))
  const completion = useRef<Promise<GoogleNotice> | null>(null)
  useEffect(() => {
    let active = true
    // Reuse the same exchange across StrictMode's repeated effect setup.
    if (!completion.current) {
      completion.current = (async (): Promise<GoogleNotice> => {
        const code = params.get('code')
        const state = params.get('state')
        let expected: string | null = null
        try {
          expected = sessionStorage.getItem(googleStateKey)
          sessionStorage.removeItem(googleStateKey)
        } catch {
          /* The daemon still enforces single-use state. */
        }
        window.history.replaceState(window.history.state, '', window.location.pathname)
        if (params.get('error') === 'access_denied') return 'denied'
        if (params.has('error') || !code || !state || (expected && expected !== state))
          return 'error'
        try {
          const status = await rpc.call('google.connect.complete', { code, state })
          client.setQueryData(googleQueryKey, status)
          await client.invalidateQueries({ queryKey: googleQueryKey })
          return status.missing_scopes?.length ? 'missing' : status.connected ? 'success' : 'error'
        } catch {
          return 'error'
        }
      })()
    }
    void completion.current.then((result) => {
      if (!active) return
      setNotice(result)
      void navigate({ to: '/settings', search: { section: 'connections' }, replace: true })
    })
    return () => {
      active = false
    }
  }, [client, navigate, params, setNotice])
  return (
    <main className="google-callback" role="status">
      <LoaderCircle className="google-spinner" aria-hidden="true" />
      <p>{t('googleConnecting')}</p>
    </main>
  )
}

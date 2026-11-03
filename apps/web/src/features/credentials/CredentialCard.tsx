import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { ShieldCheck } from 'lucide-react'
import { rpc } from '@/lib/rpc/client'
import type { CredentialField, CredentialRequest, CredentialStatus } from '@/lib/rpc/credentials'
import { Button } from '@/components/ui/button'
import { SecretInput } from './SecretInput'
import { UnlockVault } from './UnlockVault'
import { clearForm, vaultLocked } from './secure'

const labels: Record<CredentialField, string> = {
  username: 'credentialUsername',
  email: 'credentialEmail',
  password: 'credentialPassword',
  otp: 'credentialOtp',
  api_key: 'credentialApiKey',
}

export function CredentialCard({
  request,
  onResolved,
  focused = false,
}: {
  request: CredentialRequest
  onResolved: (status: Exclude<CredentialStatus, 'pending'>) => void
  focused?: boolean
}) {
  const { t } = useTranslation()
  const form = useRef<HTMLFormElement>(null)
  const card = useRef<HTMLElement>(null)
  const active = useRef(true)
  const pending = useRef(false)
  const [busy, setBusy] = useState(false)
  const [locked, setLocked] = useState(false)
  const [error, setError] = useState(false)
  const [resolved, setResolved] = useState<CredentialStatus>('pending')
  const status = request.status === 'pending' ? resolved : request.status
  const currentStatus = useRef(status)
  useLayoutEffect(() => {
    currentStatus.current = status
    const node = form.current
    return () => clearForm(node)
  }, [status])
  const hasOtp = request.fields.includes('otp')
  const [remember, setRemember] = useState(request.remember_default && !hasOtp)
  useEffect(() => {
    active.current = true
    const node = form.current
    return () => {
      active.current = false
      clearForm(node)
    }
  }, [])
  useEffect(() => {
    if (status !== 'pending') clearForm(form.current)
  }, [status])
  useEffect(() => {
    if (!focused) return
    card.current?.scrollIntoView?.({ block: 'center' })
    card.current?.focus({ preventScroll: true })
  }, [focused])
  const finish = (next: Exclude<CredentialStatus, 'pending'>) => {
    clearForm(form.current)
    setResolved(next)
    onResolved(next)
  }
  const submit = async () => {
    if (pending.current || !active.current || currentStatus.current !== 'pending') return
    const values: Partial<Record<CredentialField, string>> = {}
    for (const field of request.fields) {
      const input = form.current?.elements.namedItem(`credential-${request.id}-${field}`)
      if (input instanceof HTMLInputElement) values[field] = input.value
    }
    if (request.fields.some((field) => !values[field])) return
    pending.current = true
    setBusy(true)
    setError(false)
    try {
      const result = await rpc.call('credentials.submit', {
        request_id: request.id,
        values,
        remember: remember && !hasOtp,
      })
      if (!result.ok) throw new Error('Submit failed')
      if (active.current && currentStatus.current === 'pending') finish('provided')
    } catch (cause) {
      if (active.current) {
        if (vaultLocked(cause)) setLocked(true)
        else setError(true)
      }
    } finally {
      for (const field of request.fields) delete values[field]
      pending.current = false
      if (active.current) setBusy(false)
    }
  }
  return (
    <section
      ref={card}
      tabIndex={focused ? -1 : undefined}
      aria-label={t('credentialTitle', { site: request.site })}
      className="my-4 grid min-w-0 gap-4 rounded-xl border border-border bg-surface p-4 text-foreground focus-visible:outline-2 focus-visible:outline-primary sm:p-6"
    >
      {status !== 'pending' ? (
        <p role="status" className="flex items-center gap-2">
          <ShieldCheck size={18} aria-hidden="true" />
          {t(
            status === 'cancelled'
              ? 'credentialCancelled'
              : status === 'timeout'
                ? 'credentialTimeout'
                : 'credentialSent',
          )}
        </p>
      ) : (
        <>
          <header className="grid gap-2">
            <strong className="text-lg break-all">{request.site}</strong>
            <p className="text-muted-foreground">{request.purpose}</p>
          </header>
          <form
            ref={form}
            autoComplete="off"
            className="grid min-w-0 gap-4"
            onSubmit={(event) => {
              event.preventDefault()
              void submit()
            }}
          >
            {request.fields.map((field) => {
              const props = {
                name: `credential-${request.id}-${field}`,
                required: true,
                disabled: busy,
                spellCheck: false,
                autoCapitalize: 'none',
              }
              return field === 'username' || field === 'email' ? (
                <label key={field} className="grid min-w-0 gap-2">
                  {t(labels[field])}
                  <input
                    {...props}
                    type={field === 'email' ? 'email' : 'text'}
                    autoComplete="off"
                    className="min-w-0 rounded-md border border-border bg-background px-3 py-2"
                  />
                </label>
              ) : (
                <SecretInput
                  key={field}
                  {...props}
                  label={t(labels[field])}
                  autoComplete={field === 'otp' ? 'one-time-code' : 'new-password'}
                  inputMode={field === 'otp' ? 'numeric' : undefined}
                />
              )
            })}
            <label className="flex items-center gap-2">
              <input
                type="checkbox"
                checked={remember}
                disabled={busy || hasOtp}
                onChange={(event) => setRemember(event.target.checked)}
              />
              {t('credentialRemember')}
            </label>
            {hasOtp && <p className="text-sm text-muted-foreground">{t('credentialOtpNote')}</p>}
            <div className="flex flex-wrap gap-2">
              <Button type="submit" variant="primary" disabled={busy || locked}>
                {t('credentialSend')}
              </Button>
              <Button
                disabled={busy}
                onClick={async () => {
                  if (pending.current) return
                  pending.current = true
                  setBusy(true)
                  setError(false)
                  clearForm(form.current)
                  try {
                    await rpc.call('credentials.cancel', { request_id: request.id })
                    if (active.current) finish('cancelled')
                  } catch {
                    if (active.current) setError(true)
                  } finally {
                    pending.current = false
                    if (active.current) setBusy(false)
                  }
                }}
              >
                {t('credentialCancel')}
              </Button>
            </div>
          </form>
          {locked && (
            <div className="grid gap-3">
              <p role="status">{t('credentialLocked')}</p>
              <UnlockVault
                onUnlocked={async () => {
                  setLocked(false)
                  await submit()
                }}
              />
            </div>
          )}
          {error && <p role="alert">{t('credentialError')}</p>}
        </>
      )}
    </section>
  )
}

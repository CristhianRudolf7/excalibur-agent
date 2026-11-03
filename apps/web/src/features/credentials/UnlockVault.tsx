import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { rpc } from '@/lib/rpc/client'
import { Button } from '@/components/ui/button'
import { SecretInput } from './SecretInput'
import { clearForm } from './secure'

export function UnlockVault({ onUnlocked }: { onUnlocked: () => void | Promise<void> }) {
  const { t } = useTranslation()
  const form = useRef<HTMLFormElement>(null)
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(false)
  useEffect(() => {
    const node = form.current
    return () => clearForm(node)
  }, [])
  return (
    <form
      ref={form}
      autoComplete="off"
      className="grid gap-3"
      onSubmit={async (event) => {
        event.preventDefault()
        if (busy) return
        const input = form.current?.querySelector('input')
        if (!input?.value) return
        setBusy(true)
        setError(false)
        const params = { passphrase: input.value }
        input.value = ''
        try {
          const result = await rpc.call('vault.unlock', params)
          if (!result.unlocked) throw new Error('Unlock failed')
          params.passphrase = ''
          await onUnlocked()
        } catch {
          setError(true)
        } finally {
          params.passphrase = ''
          setBusy(false)
        }
      }}
    >
      <SecretInput
        label={t('credentialPassphrase')}
        name="vault-unlock-passphrase"
        autoComplete="new-password"
        required
        disabled={busy}
      />
      {error && <p role="alert">{t('credentialUnlockError')}</p>}
      <Button type="submit" variant="primary" disabled={busy}>
        {t('credentialUnlock')}
      </Button>
    </form>
  )
}

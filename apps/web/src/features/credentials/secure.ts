import { RpcError } from '@/lib/rpc/client'

export function vaultLocked(error: unknown): boolean {
  if (!(error instanceof RpcError)) return false
  const data = error.data
  return (
    error.message === 'vault_locked' ||
    error.message === 'vault: vault is locked' ||
    (typeof data === 'object' &&
      data !== null &&
      (('code' in data && data.code === 'vault_locked') ||
        ('reason' in data && data.reason === 'vault_locked')))
  )
}

export function clearForm(form: HTMLFormElement | null) {
  if (!form) return
  for (const input of form.querySelectorAll('input')) {
    if (input.type !== 'checkbox') input.value = ''
  }
}

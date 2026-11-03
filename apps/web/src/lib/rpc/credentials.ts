/** Dedicated secure RPCs. Values must never enter queries, mutations or chat state. */
export type CredentialField = 'username' | 'email' | 'password' | 'otp' | 'api_key'
export type CredentialStatus = 'pending' | 'provided' | 'cancelled' | 'timeout' | 'stored'
export interface CredentialRequest {
  id: string
  conversation_id: string
  site: string
  purpose: string
  fields: CredentialField[]
  remember_default: boolean
  status: CredentialStatus
  created_at: string
}
export interface CredentialRequestedEvent extends Omit<CredentialRequest, 'id' | 'status'> {
  request_id: string
  run_id: string
}
export interface CredentialEvents {
  'credentials.requested': CredentialRequestedEvent
  'credentials.resolved': { request_id: string; status: Exclude<CredentialStatus, 'pending'> }
}
export interface VaultMetadata {
  id: string
  name: string
  type: string
  metadata: Record<string, string> | null
  domains: string[] | null
  created_at: string
  updated_at: string
  expires_at?: string
}
export interface CredentialMethods {
  'credentials.pending': {
    params: { conversation_id: string }
    result: { requests: CredentialRequest[] }
  }
  'credentials.submit': {
    params: {
      request_id: string
      values: Partial<Record<CredentialField, string>>
      remember: boolean
    }
    result: { ok: boolean }
  }
  'credentials.cancel': { params: { request_id: string }; result: { ok: boolean } }
  'vault.list_metadata': {
    params: Record<string, never>
    result: { entries: VaultMetadata[] | null; locked: boolean }
  }
  'vault.unlock': { params: { passphrase: string }; result: { unlocked: boolean } }
  'vault.lock': { params: Record<string, never>; result: { locked: boolean } }
  'vault.delete': { params: { name: string; confirmed: true }; result: { deleted: string } }
}

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
export function siteHosts(entry: VaultMetadata): string[] {
  const hosts = entry.domains?.length
    ? entry.domains
    : [
        entry.metadata?.credential_site ??
          entry.metadata?.host ??
          entry.metadata?.domain ??
          entry.metadata?.site ??
          '',
      ]
  if (hosts.some(Boolean)) return hosts.filter(Boolean)
  try {
    return entry.metadata?.url ? [new URL(entry.metadata.url).host] : []
  } catch {
    return []
  }
}
/** Derive exact hosts locally; the secure RPC never accepts a URL as site. */
export function loginAddress(address: string, loginHost = '') {
  const parse = (value: string) => {
    const trimmed = value.trim()
    if (
      !trimmed ||
      /\s/.test(trimmed) ||
      (/^[a-z][a-z\d+.-]*:\/\//i.test(trimmed) && !/^https?:\/\//i.test(trimmed))
    )
      throw new Error('Invalid address')
    const full = /^https?:\/\//i.test(trimmed)
    const url = new URL(full ? trimmed : `https://${trimmed}`)
    if (
      !['http:', 'https:'].includes(url.protocol) ||
      url.username ||
      url.password ||
      !url.hostname
    )
      throw new Error('Invalid address')
    return { host: url.hostname.replace(/^\[|\]$/g, '').toLowerCase(), start: full ? trimmed : '' }
  }
  const parsed = parse(address)
  return { site: loginHost.trim() ? parse(loginHost).host : parsed.host, start_url: parsed.start }
}

export interface LoginCard {
  id: string
  label: string
  host: string[]
  entries: VaultMetadata[]
  username: boolean
  email: boolean
}
export function loginCards(entries: VaultMetadata[]): LoginCard[] {
  const cards = new Map<string, LoginCard>()
  for (const entry of entries) {
    const bundle = entry.metadata?.credential_bundle
    const id = bundle ? `bundle:${bundle}` : `entry:${entry.id}`
    let card = cards.get(id)
    if (!card) {
      const host = siteHosts(entry)
      card = {
        id,
        label: bundle
          ? entry.metadata?.credential_label ||
            entry.metadata?.credential_site ||
            host[0] ||
            entry.name
          : entry.name,
        host,
        entries: [],
        username: false,
        email: false,
      }
      cards.set(id, card)
    }
    card.entries.push(entry)
    card.username ||= entry.metadata?.credential_field === 'username'
    card.email ||= entry.metadata?.credential_field === 'email'
  }
  return [...cards.values()]
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
  'credentials.save_login': {
    params: {
      site: string
      label?: string
      start_url?: string
      values: { username?: string; email?: string; password: string }
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

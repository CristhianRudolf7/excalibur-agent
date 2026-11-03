import type { CredentialRequest } from '@/lib/rpc/credentials'

/** Explicitly retain only public metadata, even if a server adds fields later. */
export function credentialMetadata(request: CredentialRequest): CredentialRequest {
  return {
    id: request.id,
    conversation_id: request.conversation_id,
    site: request.site,
    purpose: request.purpose,
    fields: [...request.fields],
    remember_default: request.remember_default,
    status: request.status,
    created_at: request.created_at,
  }
}

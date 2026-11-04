import { expectTypeOf, it } from 'vitest'
import type { CodexStatus, RpcMethods } from '@/lib/rpc/contract'

it('keeps account credentials outside all RPC result types', () => {
  expectTypeOf<RpcMethods['codex.status']['result']>().toEqualTypeOf<CodexStatus>()
  expectTypeOf<RpcMethods['codex.login.complete']['result']>().toEqualTypeOf<CodexStatus>()
  expectTypeOf<RpcMethods['codex.logout']['result']>().toEqualTypeOf<CodexStatus>()
  expectTypeOf<keyof CodexStatus>().toEqualTypeOf<
    'connected' | 'source' | 'account_email' | 'plan_type' | 'expires_at'
  >()
  expectTypeOf<RpcMethods['codex.login.start']['result']>().toEqualTypeOf<{ auth_url: string }>()
  expectTypeOf<RpcMethods['codex.login.complete']['params']>().toEqualTypeOf<{
    code: string
    state: string
  }>()
})

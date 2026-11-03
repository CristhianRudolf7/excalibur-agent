// @vitest-environment node
/// <reference types="node" />
import { readFileSync } from 'node:fs'
import { expect, it } from 'vitest'
import { clientMethods } from './contract'

it('uses only method names exported by the daemon contract', () => {
  const path = new URL('../../../../../packages/contracts/methods.json', import.meta.url)
  const contract = JSON.parse(readFileSync(path, 'utf8')) as { methods: { name: string }[] }
  expect(clientMethods).toContain('activity.list')
  for (const method of ['credentials.pending', 'credentials.submit', 'credentials.cancel'])
    expect(clientMethods).toContain(method)
  for (const method of clientMethods)
    expect(contract.methods.map((entry) => entry.name)).toContain(method)
})

it('resolves credential schema aliases against generated Go definitions', () => {
  const base = new URL('../../../../../packages/contracts/', import.meta.url)
  const aliases = JSON.parse(readFileSync(new URL('credentials.json', base), 'utf8')) as {
    $defs: Record<string, { $ref: string }>
  }
  for (const alias of Object.values(aliases.$defs)) {
    const [file, key] = alias.$ref.split('#/$defs/')
    const generated = JSON.parse(readFileSync(new URL(file!, base), 'utf8')) as {
      $defs: Record<string, unknown>
    }
    expect(generated.$defs[key!]).toBeDefined()
  }
  for (const file of ['methods.json', 'events.json']) {
    const generated = JSON.parse(readFileSync(new URL(file, base), 'utf8')) as {
      $defs: Record<string, { required: string[] }>
    }
    expect(generated.$defs.CredentialsRequestedEvent!.required).toEqual(
      expect.arrayContaining([
        'request_id',
        'conversation_id',
        'run_id',
        'site',
        'purpose',
        'fields',
        'remember_default',
        'created_at',
      ]),
    )
    expect(generated.$defs.CredentialsResolvedEvent!.required).toEqual(['request_id', 'status'])
    expect(generated.$defs.CredentialsOKResult!.required).toEqual(['ok'])
  }
})

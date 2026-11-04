// @vitest-environment node
/// <reference types="node" />
import { readFileSync } from 'node:fs'
import { expect, expectTypeOf, it } from 'vitest'
import { clientMethods, type RpcMethods } from './contract'

it('uses only method names exported by the daemon contract', () => {
  const path = new URL('../../../../../packages/contracts/methods.json', import.meta.url)
  const contract = JSON.parse(readFileSync(path, 'utf8')) as { methods: { name: string }[] }
  expect(clientMethods).toContain('activity.list')
  for (const method of ['credentials.pending', 'credentials.submit', 'credentials.cancel'])
    expect(clientMethods).toContain(method)
  for (const method of clientMethods)
    expect(contract.methods.map((entry) => entry.name)).toContain(method)
})

it('exports the optional attempt reset skill reason consistently', () => {
  type Params = RpcMethods['robotlab.attempt.reset']['params']
  expectTypeOf<Params['no_skill_reason']>().toEqualTypeOf<string | undefined>()
  expectTypeOf<{ session: string; reflection: string }>().toExtend<Params>()
  const base = new URL('../../../../../packages/contracts/', import.meta.url)
  const source = JSON.parse(readFileSync(new URL('robotlab.json', base), 'utf8')) as {
    methods: Record<string, { params: { properties: Record<string, unknown>; required: string[] } }>
  }
  const registry = JSON.parse(readFileSync(new URL('methods.json', base), 'utf8')) as {
    methods: { name: string; params_schema: unknown }[]
  }
  const params = source.methods['robotlab.attempt.reset']!.params
  expect(params.properties.no_skill_reason).toEqual({ type: 'string', maxLength: 2000 })
  expect(params.required).toEqual(['session', 'reflection'])
  expect(
    registry.methods.find((entry) => entry.name === 'robotlab.attempt.reset')?.params_schema,
  ).toEqual(params)
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

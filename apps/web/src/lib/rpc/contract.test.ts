// @vitest-environment node
/// <reference types="node" />
import { readFileSync } from 'node:fs'
import { expect, it } from 'vitest'
import { clientMethods } from './contract'

it('uses only method names exported by the daemon contract', () => {
  const path = new URL('../../../../../packages/contracts/methods.json', import.meta.url)
  const contract = JSON.parse(readFileSync(path, 'utf8')) as { methods: { name: string }[] }
  expect(clientMethods).toContain('activity.list')
  for (const method of clientMethods)
    expect(contract.methods.map((entry) => entry.name)).toContain(method)
})

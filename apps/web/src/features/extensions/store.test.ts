import { afterEach, expect, it, vi } from 'vitest'
import { extensionCatalog } from './catalog'
import { installedExtensionsKey, readInstalledExtensions, useExtensions } from './store'

const defaults = extensionCatalog.map(({ id }) => id)
afterEach(() => {
  vi.restoreAllMocks()
  localStorage.removeItem(installedExtensionsKey)
  useExtensions.setState({ installed: [...defaults] })
})

it('defaults to all built-ins and preserves an intentionally empty installation', () => {
  localStorage.removeItem(installedExtensionsKey)
  expect(readInstalledExtensions()).toEqual(defaults)
  localStorage.setItem(installedExtensionsKey, '[]')
  expect(readInstalledExtensions()).toEqual([])
})

it.each(['broken', '{}', '[1]', 'null'])(
  'recovers invalid installation preferences: %s',
  (value) => {
    localStorage.setItem(installedExtensionsKey, value)
    expect(readInstalledExtensions()).toEqual(defaults)
  },
)

it('ignores unknown IDs and restores catalog order without duplicates', () => {
  localStorage.setItem(installedExtensionsKey, '["robot", "unknown", "robot", "classroom"]')
  expect(readInstalledExtensions()).toEqual(['classroom', 'robot'])
})

it('persists removal and idempotent reinstall so a reload keeps the same sidebar', () => {
  useExtensions.getState().setInstalled('robot', false)
  expect(useExtensions.getState().installed).toEqual(['classroom', 'robotlab'])
  expect(readInstalledExtensions()).toEqual(['classroom', 'robotlab'])
  useExtensions.getState().setInstalled('robot', true)
  useExtensions.getState().setInstalled('robot', true)
  expect(readInstalledExtensions()).toEqual(defaults)
})

it('keeps session controls usable when browser storage is blocked', () => {
  vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
    throw new Error('blocked')
  })
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new Error('blocked')
  })
  expect(readInstalledExtensions()).toEqual(defaults)
  useExtensions.getState().setInstalled('classroom', false)
  expect(useExtensions.getState().installed).toEqual(['robotlab', 'robot'])
})

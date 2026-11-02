import type { Page } from '@playwright/test'
import { mockEvents } from '../event-fixture'
import {
  sampleAutomation,
  sampleRun,
  sampleVersions,
} from '../../src/features/automations/detail/fixtures'

export async function mockAutomationDetail(page: Page) {
  const events = await mockEvents(page)
  const state = {
    automation: structuredClone(sampleAutomation),
    runs: [structuredClone(sampleRun)],
    events,
    fail: false,
    missing: false,
  }
  await page.route('**/rpc', async (route) => {
    const { id, method } = route.request().postDataJSON()
    if (!method.startsWith('automations.')) return route.fallback()
    if (state.fail) return route.abort()
    if (state.missing)
      return route.fulfill({
        json: { jsonrpc: '2.0', id, error: { code: -32000, message: 'automation not found' } },
      })
    const result =
      method === 'automations.get'
        ? { automation: state.automation, versions: sampleVersions }
        : method === 'automations.list'
          ? { automations: [state.automation] }
          : { runs: state.runs }
    return route.fulfill({ json: { jsonrpc: '2.0', id, result } })
  })
  await page.addInitScript(() => localStorage.setItem('excalibur-ui-language', 'en'))
  return state
}

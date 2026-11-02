import { mockChat } from './chat-fixture'
import { test as base, expect, type Page } from '@playwright/test'

export async function mockProfile(page: Page) {
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON() as { id: string; method: string }
    if (request.method !== 'principal.get') return route.continue()
    await route.fulfill({
      json: {
        jsonrpc: '2.0',
        id: request.id,
        result: { id: 'local', name: 'Ana Example', timezone: 'UTC', language: 'en' },
      },
    })
  })
}
export const test = base.extend({
  page: async ({ page }, providePage) => {
    await mockProfile(page)
    await mockChat(page)
    await providePage(page)
  },
})
export { expect, type Page }

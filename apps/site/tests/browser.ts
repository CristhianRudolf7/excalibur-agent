import { test as base, expect } from '@playwright/test'
import { productionBrowser, serveBuild } from './build-browser'
export { expect }
export const test = base.extend({
  browser: async ({ browser }, use) => {
    await use(productionBrowser(browser))
  },
  context: async ({ context }, use) => {
    await serveBuild(context)
    await use(context)
  },
})

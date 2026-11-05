import type { Browser, BrowserContext } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { resolve, extname, sep } from 'node:path'
import { baseURL } from './server.ts'

/** Optional production-file routing when another worker owns the assigned port.
 * Requests retain the assigned origin; only this checkout's dist is served.
 * No process, source directory, or build in another worktree is modified.
 */
export async function serveBuild(context: BrowserContext) {
  if (process.env.SITE_FROM_DISK !== '1') return
  const root = resolve('dist')
  const types: Record<string, string> = {
    '.html': 'text/html; charset=utf-8',
    '.js': 'text/javascript',
    '.css': 'text/css',
    '.woff2': 'font/woff2',
    '.svg': 'image/svg+xml',
    '.json': 'application/json',
    '.wasm': 'application/wasm',
    '.png': 'image/png',
  }
  await context.route(`${baseURL}/**`, async (route) => {
    const pathname = decodeURIComponent(new URL(route.request().url()).pathname)
    const file = resolve(root, `.${pathname.endsWith('/') ? `${pathname}index.html` : pathname}`)
    if (!file.startsWith(root + sep)) return route.fulfill({ status: 403 })
    try {
      await route.fulfill({
        contentType: types[extname(file)] ?? 'application/octet-stream',
        body: await readFile(file),
      })
    } catch {
      await route.fulfill({ status: 404, body: 'Not found in this production build' })
    }
  })
}

export function productionBrowser(browser: Browser): Browser {
  const newContext = browser.newContext.bind(browser)
  const newPage = browser.newPage.bind(browser)
  browser.newContext = async (options) => {
    const context = await newContext(options)
    await serveBuild(context)
    return context
  }
  browser.newPage = async (options) => {
    const page = await newPage(options)
    await serveBuild(page.context())
    return page
  }
  return browser
}

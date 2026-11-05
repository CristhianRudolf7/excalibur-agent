import type { BrowserContext } from '@playwright/test'
import { readFile } from 'node:fs/promises'
import { resolve, extname, sep } from 'node:path'

/** Serve this worktree's build inside Chrome, without binding another worker's port. */
export async function serveBuild(context: BrowserContext) {
  if (process.env.B_DIRECT_PREVIEW === '1') return
  const root = resolve('dist')
  const mime: Record<string, string> = {
    '.html': 'text/html',
    '.js': 'text/javascript',
    '.css': 'text/css',
    '.woff2': 'font/woff2',
    '.svg': 'image/svg+xml',
    '.json': 'application/json',
    '.png': 'image/png',
    '.ico': 'image/x-icon',
  }
  await context.route('http://127.0.0.1:4333/**', async (route) => {
    const url = new URL(route.request().url())
    const pathname = decodeURIComponent(url.pathname)
    const file = resolve(root, `.${pathname.endsWith('/') ? `${pathname}index.html` : pathname}`)
    if (!file.startsWith(root + sep)) return route.fulfill({ status: 403 })
    try {
      await route.fulfill({
        status: 200,
        contentType: mime[extname(file)] ?? 'application/octet-stream',
        body: await readFile(file),
      })
    } catch {
      await route.fulfill({ status: 404, body: 'Not found' })
    }
  })
}

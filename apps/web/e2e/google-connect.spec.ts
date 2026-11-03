import { test, expect, type Page } from '@playwright/test'
import { defaultAgent } from '../src/features/agents/store'
import type { GoogleStatus } from '../src/lib/rpc/contract'

async function fakeDaemon(page: Page, partial = false) {
  let status: GoogleStatus = { connected: false, configured: true }
  let exchanges = 0
  await page.addInitScript(() => localStorage.setItem('excalibur-ui-language', 'en'))
  await page.routeWebSocket('**/ws', (socket) => socket.close())
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    let result: unknown = {}
    switch (request.method) {
      case 'principal.get':
      case 'principal.update':
        result = {
          id: 'local',
          name: 'Example',
          timezone: 'UTC',
          timezone_source: 'manual',
          language: 'en',
        }
        break
      case 'agents.list':
        result = { agents: [defaultAgent], favorite_id: defaultAgent.id }
        break
      case 'agents.mark_read':
        result = { unread: 0 }
        break
      case 'conversation.history':
        result = { messages: [] }
        break
      case 'conversation.subscribe':
        result = { cursor: 0, events: [] }
        break
      case 'google.status':
        result = status
        break
      case 'google.connect.start':
        result = {
          auth_url: `${new URL(route.request().url()).origin}/fake-google-consent?state=fake-state&code_challenge=fake-challenge`,
        }
        break
      case 'google.connect.complete':
        expect(request.params).toEqual({ code: 'fake-code', state: 'fake-state' })
        exchanges++
        status = {
          connected: true,
          configured: true,
          email: 'person@example.test',
          scopes: [
            'https://www.googleapis.com/auth/drive',
            ...(partial
              ? []
              : ['https://mail.google.com/', 'https://www.googleapis.com/auth/documents']),
          ],
          missing_scopes: partial
            ? ['https://mail.google.com/', 'https://www.googleapis.com/auth/documents']
            : [],
          expires_at: new Date(Date.now() + 24 * 3_600_000).toISOString(),
        }
        result = status
        break
      case 'google.disconnect':
        status = { connected: false, configured: true }
        result = status
        break
    }
    await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result } })
  })
  await page.route('**/fake-google-consent?**', async (route) => {
    await route.fulfill({
      contentType: 'text/html',
      body: '<!doctype html><html lang="en"><body><a href="/oauth/google/callback?code=fake-code&amp;state=fake-state">Grant Google access</a></body></html>',
    })
  })
  return () => exchanges
}

test('connect in the same tab, complete the SPA callback, show account and confirm disconnect', async ({
  page,
}) => {
  const exchanges = await fakeDaemon(page)
  await page.goto('/settings?section=connections')
  await expect(page.getByText('No account connected', { exact: true })).toBeVisible()
  await page.getByRole('button', { name: 'Connect Google' }).click()
  await expect(page).toHaveURL(/fake-google-consent/)
  await page.getByRole('link', { name: 'Grant Google access' }).click()
  await expect(page).toHaveURL(/settings\?section=connections/)
  await expect(page.getByText('Connected as person@example.test')).toBeVisible()
  await expect(page.getByText('Your Google account is connected.')).toBeVisible()
  for (const name of ['Gmail', 'Drive', 'Docs'])
    await expect(
      page.getByRole('list', { name: 'Granted permissions' }).getByText(name, { exact: true }),
    ).toBeVisible()
  await expect(page.getByText(/Less than two days remain/)).toBeVisible()
  expect(exchanges()).toBe(1)
  await page.getByRole('button', { name: 'Disconnect', exact: true }).click()
  await expect(page.getByRole('dialog')).toBeVisible()
  await page.getByRole('dialog').getByRole('button', { name: 'Cancel' }).click()
  await expect(page.getByText('Connected as person@example.test')).toBeVisible()
  await page.getByRole('button', { name: 'Disconnect', exact: true }).click()
  await page.getByRole('dialog').getByRole('button', { name: 'Disconnect', exact: true }).click()
  await expect(page.getByText('No account connected', { exact: true })).toBeVisible()
})

test('partial consent names missing permissions and offers reconnect', async ({ page }) => {
  await fakeDaemon(page, true)
  await page.goto('/settings?section=connections')
  await page.getByRole('button', { name: 'Connect Google' }).click()
  await page.getByRole('link', { name: 'Grant Google access' }).click()
  await expect(page.getByText(/Missing permissions: Gmail, Docs/)).toBeVisible()
  await expect(page.getByRole('button', { name: 'Reconnect', exact: true })).toBeVisible()
})

test('denied SPA callback returns useful feedback without exchanging a code', async ({ page }) => {
  const exchanges = await fakeDaemon(page)
  await page.goto('/oauth/google/callback?error=access_denied&state=fake-state')
  await expect(page).toHaveURL(/settings\?section=connections/)
  await expect(page.getByText('Google access was not granted. You can try again.')).toBeVisible()
  expect(exchanges()).toBe(0)
})

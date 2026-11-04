import { expect, test } from '@playwright/test'

test('connects a remote account and disconnects from Settings', async ({ page }) => {
  let connected = false
  let completed = false
  await page.addInitScript(() => localStorage.setItem('excalibur-ui-language', 'en'))
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON() as {
      id: string
      method: string
      params?: { code?: string; state?: string }
    }
    let result: unknown = {}
    if (request.method === 'codex.login.start') {
      result = { auth_url: 'https://auth.example.test/oauth/authorize?state=browser-state' }
    } else if (request.method.startsWith('codex.')) {
      if (request.method === 'codex.login.complete') {
        expect(request.params?.state).toBe('browser-state')
        expect(request.params?.code).toBe(
          'http://localhost:1455/auth/callback?code=browser-code&state=browser-state',
        )
        completed = true
        connected = true
      }
      if (request.method === 'codex.logout') connected = false
      result = {
        connected,
        source: connected ? 'excalibur' : '',
        account_email: connected ? 'person@example.test' : '',
        plan_type: connected ? 'plus' : '',
      }
    } else if (request.method === 'google.status') {
      result = { connected: false, configured: false }
    } else if (request.method === 'principal.get') {
      result = { id: 'local', name: 'Test User', timezone: 'UTC', language: 'en' }
    } else if (request.method === 'agents.list') {
      result = { agents: [], favorite_id: '' }
    } else if (request.method === 'work.list') {
      result = { tasks: [], total: 0 }
    }
    await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result } })
  })
  await page.goto('/settings?section=connections')
  const card = page.getByRole('region', { name: 'ChatGPT / Codex account' })
  await expect(card.getByText('No account connected')).toBeVisible()
  await card.getByRole('button', { name: 'Connect', exact: true }).click()
  await expect(card.getByRole('link', { name: 'Open Sign in with ChatGPT' })).toBeVisible()
  await card
    .getByLabel('Final redirect URL or code')
    .fill('http://localhost:1455/auth/callback?code=browser-code&state=browser-state')
  await card.getByRole('button', { name: 'Complete connection' }).click()
  await expect(card.getByText('Connected as person@example.test')).toBeVisible()
  await expect(card.getByText('Plan: plus')).toBeVisible()
  await expect(card.getByText('Source: Excalibur')).toBeVisible()
  expect(completed).toBe(true)
  await card.getByRole('button', { name: 'Disconnect', exact: true }).click()
  await expect(card.getByText('No account connected')).toBeVisible()
  await expect(card.getByText('Usage counts against your ChatGPT plan limits.')).toBeVisible()
})

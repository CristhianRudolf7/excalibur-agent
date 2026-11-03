import { test, expect } from '@playwright/test'
import { defaultAgent } from '../src/features/agents/store'
import type { Principal } from '../src/lib/rpc/contract'

test.use({ timezoneId: 'America/Lima' })

for (const language of ['en', 'es']) {
  test(`time zone Settings persists manual choices and explicitly restores detection (${language})`, async ({
    page,
  }) => {
    let principal: Principal = {
      id: 'local',
      name: 'Example',
      timezone: 'UTC',
      timezone_source: 'default',
      language,
    }
    const updates: { timezone: string; timezone_source: string; enable_auto?: boolean }[] = []
    await page.addInitScript(
      (lang) => localStorage.setItem('excalibur-ui-language', lang),
      language,
    )
    await page.route('**/rpc', async (route) => {
      const request = route.request().postDataJSON()
      let result: unknown = {}
      switch (request.method) {
        case 'principal.get':
          result = principal
          break
        case 'principal.update': {
          updates.push(request.params)
          if (
            request.params.timezone_source !== 'auto' ||
            principal.timezone_source !== 'manual' ||
            request.params.enable_auto
          )
            principal = { ...principal, ...request.params }
          result = principal
          break
        }
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
          result = { cursor: 0 }
          break
      }
      await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result } })
    })
    await page.goto('/settings')
    const english = language === 'en'
    const picker = page.getByRole('button', {
      name: english ? 'Time zone' : 'Zona horaria',
      exact: true,
    })
    const toggle = page.getByRole('switch', {
      name: english ? 'Detect automatically' : 'Detectar automáticamente',
    })
    await expect(picker).toHaveText('America/Lima')
    await expect(toggle).toBeChecked()
    expect(updates).toEqual([{ timezone: 'America/Lima', timezone_source: 'auto' }])
    await picker.click()
    const search = page.getByRole('textbox', {
      name: english ? 'Search time zones' : 'Buscar zona horaria',
    })
    await expect(search).toBeFocused()
    await search.fill('Madrid')
    await search.press('ArrowDown')
    await expect(page.getByRole('button', { name: 'Europe/Madrid', exact: true })).toBeFocused()
    await page.keyboard.press('Enter')
    await expect(picker).toHaveText('Europe/Madrid')
    await expect(toggle).not.toBeChecked()
    await expect(
      page.getByLabel(english ? 'Current local time' : 'Hora local actual'),
    ).toContainText(/GMT\+0[12]:00/)
    await expect(picker).toBeFocused()
    await page.reload()
    await expect(picker).toHaveText('Europe/Madrid')
    await expect(toggle).not.toBeChecked()
    expect(updates.filter((update) => update.timezone_source === 'auto')).toHaveLength(2)
    await toggle.click()
    await expect(picker).toHaveText('America/Lima')
    await expect(toggle).toBeChecked()
    expect(updates.at(-1)).toEqual({
      timezone: 'America/Lima',
      timezone_source: 'auto',
      enable_auto: true,
    })
    await expect(
      page.getByLabel(english ? 'Current local time' : 'Hora local actual'),
    ).toContainText('GMT-05:00')
    await page.setViewportSize({ width: 390, height: 844 })
    expect(
      await page
        .locator('.settings-page')
        .evaluate((element) => element.scrollWidth <= element.clientWidth),
    ).toBe(true)
  })
}

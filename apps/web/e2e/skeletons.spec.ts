import { test, expect } from './fixtures'

for (const viewport of [
  { width: 1440, height: 900 },
  { width: 390, height: 844 },
]) {
  for (const area of ['chat', 'skills', 'usage', 'profile'] as const) {
    test(`${area} skeleton at ${viewport.width}`, async ({ page }) => {
      await page.setViewportSize(viewport)
      let release!: () => void
      const pending = new Promise<void>((resolve) => {
        release = resolve
      })
      const method = {
        chat: 'conversation.history',
        skills: 'skills.list',
        usage: 'usage.summary',
        profile: 'principal.get',
      }[area]
      await page.route('**/rpc', async (route) => {
        const request = route.request().postDataJSON()
        if (request.method !== method) return route.fallback()
        await pending
        const result =
          area === 'chat'
            ? {
                messages: [
                  {
                    id: 'loaded',
                    conversation_id: 'cnv_web_excalibur',
                    role: 'assistant',
                    text: 'History has arrived',
                  },
                ],
              }
            : area === 'profile'
              ? { id: 'local', name: 'Skeleton Tester', timezone: 'UTC', language: 'en' }
              : area === 'skills'
                ? { skills: [] }
                : {
                    totals: {
                      input_tokens: 0,
                      cached_tokens: 0,
                      output_tokens: 0,
                      cost_usd: 0,
                      tool_calls: 0,
                      runs: 0,
                    },
                    by_bucket: [],
                    by_model: [],
                    by_tool: [],
                    recent_runs: [],
                  }
        await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result } })
      })
      if (area === 'skills') {
        await page.route('**/rpc', async (route) => {
          const request = route.request().postDataJSON()
          if (request.method !== 'skills.groups') return route.fallback()
          await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result: { groups: [] } } })
        })
      }
      await page.goto(
        area === 'skills' || area === 'usage'
          ? `/settings?section=${area}`
          : '/agents/excalibur?view=chat',
      )
      if (area === 'profile') {
        if (viewport.width === 390)
          await page
            .locator('.chat-header')
            .getByRole('button', { name: 'Alternar barra lateral' })
            .click()
        await page.locator('.sidebar').getByRole('button', { name: 'Perfil', exact: true }).click()
      }
      const selector = {
        chat: '.sk-history',
        skills: '.sk-skills',
        usage: '.sk-usage',
        profile: '.profile-details.loading-layout',
      }[area]
      const skeleton = page.locator(selector).first()
      await expect(skeleton).toBeVisible()
      await expect(skeleton).toHaveAttribute('role', 'status')
      await expect(skeleton).toHaveAttribute('aria-busy', 'true')
      await expect(skeleton).toHaveAccessibleName(
        area === 'chat'
          ? 'Cargando mensajes'
          : area === 'profile'
            ? 'Cargando perfil'
            : 'Cargando…',
      )
      await expect(skeleton.locator('.skeleton').first()).toBeVisible()
      await expect(
        page.getByText(/^(Cargando…|Loading…|Cargando editor…|Cargando pizarras…)$/),
      ).toHaveCount(0)
      await page.screenshot({ path: `sk-shots/sk1-${area}-${viewport.width}.png` })
      if (area === 'usage' && viewport.width === 1440) {
        await page.evaluate(() => document.documentElement.classList.add('dark'))
        await page.screenshot({ path: 'sk-shots/sk1-usage-dark.png' })
      }
      release()
      await expect(skeleton).toHaveCount(0)
      if (area === 'chat') await expect(page.getByText('History has arrived')).toBeVisible()
      if (area === 'profile')
        await expect(page.getByRole('dialog').getByText('Skeleton Tester')).toBeVisible()
      if (area === 'skills')
        await expect(
          page.locator('.settings-content').getByText('Todavía no hay datos.', { exact: true }),
        ).toBeVisible()
      if (area === 'usage') await expect(page.locator('.usage-kpis')).toBeVisible()
    })
  }
}

for (const width of [1440, 390]) {
  for (const area of ['settings', 'world', 'editor', 'chart'] as const) {
    test(`lazy ${area} skeleton at ${width}`, async ({ page }) => {
      await page.setViewportSize({ width, height: width === 390 ? 844 : 900 })
      let release!: () => void
      const pending = new Promise<void>((resolve) => {
        release = resolve
      })
      const chunk = {
        settings: 'Settings',
        world: 'World',
        editor: 'CodeEditor',
        chart: 'UsageChart',
      }[area]
      await page.route(new RegExp(`/assets/${chunk}-[^/]+\\.js$`), async (route) => {
        await pending
        await route.continue()
      })
      const skill = {
        name: 'skeleton-skill',
        description: 'A skill fixture',
        source: 'user',
        state: 'enabled',
        version: '1',
        editable: true,
        deletable: true,
        tools: [],
        content: '# Instructions',
        diagnostics: [],
        path_kind: 'user',
      }
      await page.route('**/rpc', async (route) => {
        const request = route.request().postDataJSON()
        const totals = {
          input_tokens: 100,
          cached_tokens: 0,
          output_tokens: 10,
          cost_usd: 1,
          tool_calls: 1,
          runs: 1,
        }
        const results: Record<string, unknown> = {
          'skills.list': { skills: [skill] },
          'skills.groups': { groups: [] },
          'skills.get': skill,
          'usage.summary': {
            totals,
            by_bucket: [{ ...totals, start: '2026-11-01T00:00:00Z' }],
            by_model: [],
            by_tool: [],
            recent_runs: [],
          },
        }
        if (!(request.method in results)) return route.fallback()
        await route.fulfill({
          json: { jsonrpc: '2.0', id: request.id, result: results[request.method] },
        })
      })
      await page.goto(
        area === 'world'
          ? '/agents/excalibur?view=world'
          : `/settings?section=${area === 'editor' ? 'skills' : area === 'chart' ? 'usage' : 'general'}`,
        { waitUntil: 'domcontentloaded' },
      )
      if (area === 'editor') {
        await page.getByRole('button', { name: /skeleton-skill A skill fixture/ }).click()
        await page.getByRole('dialog').getByRole('button', { name: 'Editar', exact: true }).click()
      }
      if (area === 'chart') await expect(page.locator('.usage-kpis .usage-kpi')).toHaveCount(5)
      const selector = {
        settings: '.sk-settings-page',
        world: '.sk-world',
        editor: '.settings-fields-skeleton:has(.sk-editor)',
        chart: '.sk-chart',
      }[area]
      const skeleton = page.locator(selector).first()
      await expect(skeleton).toBeVisible()
      await expect(skeleton).toHaveAttribute('role', 'status')
      await expect(skeleton).toHaveAttribute('aria-busy', 'true')
      await expect(skeleton).toHaveAccessibleName(
        area === 'world'
          ? 'Preparando el campus…'
          : area === 'editor'
            ? 'Cargando editor…'
            : 'Cargando…',
      )
      await expect(skeleton).toHaveText('')
      await skeleton.scrollIntoViewIfNeeded()
      await page.screenshot({ path: `sk-shots/sk1-${area}-${width}.png` })
      await page.emulateMedia({ reducedMotion: 'reduce' })
      expect(
        await skeleton
          .locator('.skeleton')
          .first()
          .evaluate((element) => getComputedStyle(element, '::after').animationName),
      ).toBe('none')
      release()
      await expect(skeleton).toHaveCount(0, { timeout: 45000 })
      if (area === 'settings') await expect(page.locator('.settings-general')).toBeVisible()
      if (area === 'editor') await expect(page.locator('.monaco-editor')).toBeVisible()
      if (area === 'chart') await expect(page.locator('.usage-chart')).toBeVisible()
    })
  }
}

test('older chat messages keep their button label and show two pending message rows', async ({
  page,
}) => {
  let release!: () => void
  const pending = new Promise<void>((resolve) => {
    release = resolve
  })
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method !== 'conversation.history') return route.fallback()
    if (request.params.before) await pending
    const messages = request.params.before
      ? [
          {
            id: 'older-message',
            conversation_id: 'cnv_web_excalibur',
            role: 'assistant',
            text: 'Earlier history arrived',
          },
        ]
      : Array.from({ length: 50 }, (_, i) => ({
          id: `message-${i}`,
          conversation_id: 'cnv_web_excalibur',
          role: 'assistant',
          text: `Existing history ${i}`,
        }))
    await route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result: { messages } } })
  })
  await page.goto('/agents/excalibur?view=chat')
  const button = page.getByRole('button', { name: 'Cargar mensajes anteriores', exact: true })
  await button.click()
  await expect(button).toHaveAttribute('aria-busy', 'true')
  await expect(button).toHaveAccessibleName('Cargar mensajes anteriores')
  await expect(page.locator('.sk-history .sk-message')).toHaveCount(2)
  await expect(page.getByText('Existing history 0', { exact: true })).toBeVisible()
  release()
  await expect(page.locator('.sk-history')).toHaveCount(0)
  await expect(page.getByText('Earlier history arrived')).toBeVisible()
})

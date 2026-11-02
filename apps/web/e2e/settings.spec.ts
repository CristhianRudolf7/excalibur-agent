import { test, expect, type Page } from '@playwright/test'
import type { SkillSummary, UsageSummary } from '../src/lib/rpc/contract'
const initial: SkillSummary[] = [
  {
    name: 'review-code',
    group: 'code',
    description: 'Review changes carefully',
    version: '1.0',
    source: 'builtin',
    state: 'enabled',
    editable: false,
    deletable: false,
    tools: ['read_file'],
  },
  {
    name: 'daily-notes',
    group: 'writing',
    description: 'Keep a daily journal',
    version: '2.0',
    source: 'user',
    state: 'enabled',
    editable: true,
    deletable: true,
    tools: [],
  },
]
async function mock(page: Page, failState = false, failDelete = false) {
  let skills = structuredClone(initial)
  const contents = new Map<string, string>()
  let invalid = true
  const requests: { method: string; params: Record<string, unknown> }[] = []
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    requests.push(request)
    let result: unknown = {}
    let error: unknown
    switch (request.method) {
      case 'principal.get':
        result = { id: 'local', name: 'Ana Example', timezone: 'UTC', language: 'en' }
        break
      case 'skills.groups':
        result = {
          groups: [
            {
              id: 'code',
              name_en: 'Programming',
              name_es: 'Programación',
              description_en: '',
              description_es: '',
              count: 1,
            },
            {
              id: 'writing',
              name_en: 'Writing',
              name_es: 'Redacción',
              description_en: '',
              description_es: '',
              count: 1,
            },
          ],
        }
        break
      case 'skills.list':
        result = { skills }
        break
      case 'skills.get':
        result = {
          ...skills.find((s) => s.name === request.params.name),
          content:
            contents.get(request.params.name) ?? '# Skill instructions\n\nUse **careful** steps.',
          path_kind: 'user',
          diagnostics: [],
        }
        break
      case 'skills.update':
        if (invalid) {
          invalid = false
          error = {
            code: -32602,
            message: 'Invalid skill',
            data: { diagnostics: ['Missing description in frontmatter'] },
          }
        } else {
          contents.set(request.params.name, request.params.content)
          skills = skills.map((s) =>
            s.name === request.params.name
              ? { ...s, source: 'user', editable: true, deletable: true }
              : s,
          )
          result = skills.find((s) => s.name === request.params.name)
        }
        break
      case 'skills.set_state':
        if (failState) error = { code: -32603, message: 'Cannot persist' }
        else {
          skills = skills.map((s) =>
            s.name === request.params.name ? { ...s, state: request.params.state } : s,
          )
          result = skills.find((s) => s.name === request.params.name)
        }
        break
      case 'skills.delete':
        if (failDelete) {
          error = { code: -32603, message: 'Cannot delete' }
          break
        }
        skills = skills.filter((s) => s.name !== request.params.name)
        result = { name: request.params.name, deleted: true }
        break
      case 'usage.summary':
        result = {
          totals: {
            input_tokens: 12345,
            cached_tokens: 6000,
            output_tokens: 987,
            cost_usd: 1.25,
            tool_calls: 42,
            runs: 1,
          },
          by_bucket: [
            {
              start: '2026-11-01T00:00:00Z',
              input_tokens: 12345,
              cached_tokens: 6000,
              output_tokens: 987,
              cost_usd: 1.25,
              tool_calls: 42,
            },
          ],
          by_model: [
            { model: 'fixture-model', input_tokens: 12345, output_tokens: 987, cost_usd: 1.25 },
          ],
          by_tool: [{ tool: 'read_file', calls: 42, failures: 2 }],
          recent_runs: [
            {
              run_id: 'run-fixture',
              started_at: '2026-11-01T00:00:00Z',
              duration_ms: 250,
              input_tokens: 12345,
              output_tokens: 987,
              cost_usd: 1.25,
              tool_calls: 42,
              status: 'done',
            },
          ],
        } satisfies UsageSummary
        break
      default:
        error = { code: -32601, message: 'Method not found' }
    }
    await route.fulfill({
      json: { jsonrpc: '2.0', id: request.id, ...(error ? { error } : { result }) },
    })
  })
  return requests
}
test('language moves to settings and persists with theme and read-only profile', async ({
  page,
}) => {
  await mock(page)
  await page.goto('/settings')
  await expect(page.getByRole('button', { name: 'Cambiar idioma' })).toHaveCount(0)
  await page
    .getByRole('group', { name: 'Idioma de la interfaz' })
    .getByRole('button', { name: 'English' })
    .click()
  await expect(page.getByRole('heading', { name: 'Settings', exact: true })).toBeVisible()
  await page
    .getByRole('group', { name: 'Appearance' })
    .getByRole('button', { name: 'Dark', exact: true })
    .click()
  await page.reload()
  await expect(
    page
      .getByRole('group', { name: 'Interface language' })
      .getByRole('button', { name: 'English' }),
  ).toHaveAttribute('aria-pressed', 'true')
  await expect(page.locator('html')).toHaveClass('dark')
  await page
    .getByRole('navigation', { name: 'Settings' })
    .getByRole('button', { name: 'Profile' })
    .click()
  await expect(page.getByLabel('Name · Read only')).toHaveValue('Ana Example')
  await expect(page.getByLabel('Name · Read only')).toHaveAttribute('readonly', '')
})
test('usage shows real fixture metrics, accessible chart and range requests', async ({ page }) => {
  const requests = await mock(page)
  await page.goto('/settings?section=usage')
  await expect(page.locator('.usage-kpis')).toContainText('12.345')
  await expect(page.getByRole('img', { name: 'Actividad de tokens' }).locator('svg')).toBeVisible()
  await expect(page.getByText('fixture-model')).toBeVisible()
  await expect(page.getByText('run-fixture')).toBeVisible()
  await page.getByRole('group', { name: 'Intervalo' }).getByRole('button', { name: '24 H' }).click()
  await expect
    .poll(() => requests.filter((r) => r.method === 'usage.summary').at(-1)?.params.bucket)
    .toBe('hour')
  await page
    .getByRole('group', { name: 'Intervalo' })
    .getByRole('button', { name: '30 días' })
    .click()
  await expect
    .poll(() => requests.filter((r) => r.method === 'usage.summary').at(-1)?.params.bucket)
    .toBe('day')
})
test('catalog filters, markdown, edit diagnostics, save and confirmed delete', async ({ page }) => {
  const requests = await mock(page)
  await page.goto('/settings?section=skills')
  await expect(page.getByRole('heading', { name: 'Programación' })).toBeVisible()
  await page.getByRole('combobox', { name: 'Origen', exact: true }).click()
  await page.getByRole('option', { name: 'Usuario', exact: true }).click()
  await expect(page.getByRole('button', { name: /review-code Review/ })).toHaveCount(0)
  await page.getByRole('combobox', { name: 'Origen', exact: true }).click()
  await page.getByRole('option', { name: 'Todos', exact: true }).click()
  await page.getByLabel('Buscar skills').fill('journal')
  await expect(page.getByRole('button', { name: /daily-notes Keep/ })).toBeVisible()
  await page.getByRole('button', { name: /daily-notes Keep/ }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('heading', { name: 'Skill instructions' })).toBeVisible()
  await dialog.getByRole('button', { name: 'Editar', exact: true }).click()
  const editor = dialog.getByRole('textbox', { name: 'Editor de SKILL.md' })
  await expect(editor).toBeVisible()
  await editor.focus()
  await page.keyboard.press('ControlOrMeta+a')
  await page.keyboard.insertText('# Edited instructions')
  await dialog.getByRole('button', { name: 'Guardar', exact: true }).click()
  await expect(dialog.getByRole('alert')).toContainText('Missing description')
  await dialog.getByRole('button', { name: 'Guardar', exact: true }).click()
  await expect(dialog.getByRole('status')).toContainText('Cambios guardados')
  await expect(dialog.getByRole('heading', { name: 'Edited instructions' })).toBeVisible()
  expect(requests.filter((r) => r.method === 'skills.update').at(-1)?.params.content).toContain(
    'Edited instructions',
  )
  await dialog.getByRole('button', { name: 'Eliminar', exact: true }).click()
  expect(requests.filter((r) => r.method === 'skills.delete')).toHaveLength(0)
  await dialog
    .getByRole('region', { name: 'Eliminar skill' })
    .getByRole('button', { name: 'Eliminar', exact: true })
    .click()
  await expect(dialog).toHaveCount(0)
  await expect(page.locator('.settings-content')).toContainText('Todavía no hay datos')
})
test('builtin edit warns about override and disable uses set_state', async ({ page }) => {
  const requests = await mock(page)
  await page.goto('/settings?section=skills')
  await page.getByRole('button', { name: /review-code Review/ }).click()
  const dialog = page.getByRole('dialog')
  await expect(dialog.getByRole('button', { name: 'Eliminar', exact: true })).toHaveCount(0)
  await dialog.getByRole('button', { name: 'Editar', exact: true }).click()
  await expect(dialog.getByText('Se creará una copia de usuario que la reemplaza.')).toBeVisible()
  await dialog.getByRole('button', { name: 'Cancelar', exact: true }).click()
  await dialog.getByRole('button', { name: 'Desactivar review-code', exact: true }).click()
  await expect(
    dialog.getByRole('button', { name: 'Desactivar review-code', exact: true }),
  ).toBeDisabled()
  expect(requests.find((r) => r.method === 'skills.set_state')?.params).toEqual({
    name: 'review-code',
    state: 'disabled',
  })
})
test('failed toggle rolls back and offline endpoints show retry', async ({ page }) => {
  await mock(page, true)
  await page.goto('/settings?section=skills')
  await page.getByRole('switch', { name: 'Desactivar review-code' }).click()
  await expect(page.getByRole('alert')).toBeVisible()
  await expect(page.getByRole('switch', { name: 'Desactivar review-code' })).toBeChecked()
  await page.route('**/rpc', (route) =>
    route.fulfill({
      json: {
        jsonrpc: '2.0',
        id: route.request().postDataJSON().id,
        error: { code: -32601, message: 'Method not found' },
      },
    }),
  )
  await page.goto('/settings?section=usage')
  await expect(page.getByRole('alert')).toContainText('No se pudo conectar')
  await expect(page.getByRole('button', { name: 'Reintentar' })).toBeVisible()
})
test('settings stays inside a 390px viewport', async ({ page }) => {
  await mock(page)
  await page.setViewportSize({ width: 390, height: 844 })
  await page.goto('/settings?section=usage')
  await expect(page.getByRole('heading', { name: 'Consumo' })).toBeVisible()
  expect(
    await page.locator('.settings-page').evaluate((node) => node.scrollWidth <= node.clientWidth),
  ).toBe(true)
})

test('delete failure restores the card and keeps the confirmation open', async ({ page }) => {
  await mock(page, false, true)
  await page.goto('/settings?section=skills')
  await page.getByRole('button', { name: /daily-notes Keep/ }).click()
  const dialog = page.getByRole('dialog')
  await dialog.getByRole('button', { name: 'Eliminar', exact: true }).click()
  await dialog
    .getByRole('region', { name: 'Eliminar skill' })
    .getByRole('button', { name: 'Eliminar', exact: true })
    .click()
  await expect(dialog.getByRole('alert')).toContainText('No se pudo conectar')
  await dialog.getByRole('button', { name: 'Cerrar', exact: true }).first().click()
  await expect(page.getByRole('button', { name: /daily-notes Keep/ })).toBeVisible()
})
test('empty usage has no invented activity and transport failure has retry', async ({ page }) => {
  await mock(page)
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method !== 'usage.summary') return route.fallback()
    await route.fulfill({
      json: {
        jsonrpc: '2.0',
        id: request.id,
        result: {
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
        },
      },
    })
  })
  await page.goto('/settings?section=usage')
  await expect(page.locator('.usage-kpis')).toBeVisible()
  await expect(page.getByText('sin actividad en el período', { exact: true })).toHaveCount(5)
  await expect(page.locator('.usage-sparkline-flat')).toHaveCount(5)
  await expect(page.getByRole('img', { name: 'Actividad de tokens' })).toHaveCount(0)
  await expect(page.getByText('Todavía no hay datos.')).toHaveCount(4)
  await expect(page.locator('.usage-ghost')).toHaveCount(4)
  await expect(page.locator('.usage-ghost').first()).toHaveAttribute('aria-hidden', 'true')
  await page.route('**/rpc', (route) => route.abort())
  await page.goto('/settings?section=skills')
  await expect(page.locator('.settings-content').getByRole('alert')).toHaveCount(2)
})

test('category chips filter cards and the keyboard action menu opens the editor', async ({
  page,
}) => {
  await mock(page)
  await page.goto('/settings?section=skills')
  await expect(page.locator('.skills-summary')).toContainText('2 / 2')
  const categories = page.getByRole('group', { name: 'Categorías' })
  await categories.getByRole('button', { name: 'Redacción 1' }).click()
  await expect(page.getByRole('button', { name: /review-code Review/ })).toHaveCount(0)
  const menu = page.getByLabel('Acciones de daily-notes', { exact: true })
  await menu.focus()
  await page.keyboard.press('Enter')
  await page.locator('.skill-menu').getByRole('button', { name: 'Editar' }).click()
  await expect(
    page.getByRole('dialog').getByRole('textbox', { name: 'Editor de SKILL.md' }),
  ).toBeVisible()
  await expect(
    page.getByRole('dialog').getByRole('button', { name: 'Guardar', exact: true }),
  ).toBeVisible()
})

test('settings visual review at desktop and mobile with reduced motion and dark charts', async ({
  page,
}, testInfo) => {
  await mock(page)
  test.setTimeout(180_000)
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method === 'skills.groups')
      return route.fulfill({
        json: {
          jsonrpc: '2.0',
          id: request.id,
          result: {
            groups: [
              {
                id: 'code',
                name_en: 'Programming',
                name_es: 'Programación',
                description_en: '',
                description_es: '',
                count: 2,
              },
              {
                id: 'writing',
                name_en: 'Writing',
                name_es: 'Redacción',
                description_en: '',
                description_es: '',
                count: 2,
              },
            ],
          },
        },
      })
    if (request.method === 'skills.list') {
      return route.fulfill({
        json: {
          jsonrpc: '2.0',
          id: request.id,
          result: {
            skills: [
              ...initial,
              {
                ...initial[0],
                name: 'trace-dependencies',
                description: 'Understand how modules fit together.',
              },
              {
                ...initial[1],
                name: 'project-brief',
                description: 'Turn ideas into a focused project brief.',
              },
            ],
          },
        },
      })
    }
    if (request.method !== 'usage.summary') return route.fallback()
    const buckets = Array.from({ length: 14 }, (_, index) => ({
      start: new Date(Date.UTC(2026, 9, 19 + index)).toISOString(),
      input_tokens: 1200 + ((index * 791) % 4000),
      cached_tokens: 800 + ((index * 167) % 1600),
      output_tokens: 400 + ((index * 97) % 1000),
      cost_usd: 0.1 + index * 0.03,
      tool_calls: 2 + index,
    }))
    await route.fulfill({
      json: {
        jsonrpc: '2.0',
        id: request.id,
        result: {
          totals: {
            input_tokens: 42980,
            cached_tokens: 18620,
            output_tokens: 9240,
            cost_usd: 18.42,
            tool_calls: 128,
            runs: 24,
          },
          by_bucket: buckets,
          by_model: [
            { model: 'claude-sonnet', input_tokens: 26000, output_tokens: 5200, cost_usd: 12.84 },
            { model: 'gpt-5', input_tokens: 16980, output_tokens: 4040, cost_usd: 5.58 },
          ],
          by_tool: [
            { tool: 'read_file', calls: 72, failures: 0 },
            { tool: 'terminal', calls: 38, failures: 1 },
            { tool: 'edit_file', calls: 18, failures: 0 },
          ],
          recent_runs: [
            {
              run_id: 'run-024',
              task_short_code: 'EX-024',
              started_at: '2026-11-01T12:30:00Z',
              duration_ms: 2400,
              input_tokens: 4200,
              output_tokens: 840,
              cost_usd: 1.24,
              tool_calls: 12,
              status: 'done',
            },
          ],
        },
      },
    })
  })
  for (const viewport of [
    { width: 1440, height: 900 },
    { width: 390, height: 844 },
  ]) {
    await page.setViewportSize(viewport)
    for (const section of ['general', 'profile', 'usage', 'skills']) {
      await page.goto(`/settings?section=${section}`)
      if (section === 'usage') {
        await expect(
          page.getByRole('img', { name: 'Actividad de tokens' }).locator('svg'),
        ).toBeVisible()
        await expect(page.locator('.usage-cost')).toContainText('18,42')
        const timeline = page.getByRole('img', { name: 'Actividad de tokens' })
        await expect
          .poll(async () =>
            timeline.locator('svg').evaluate((svg) => svg.getBoundingClientRect().width),
          )
          .toBeGreaterThan(200)
        const chartColor = await page.evaluate(() =>
          getComputedStyle(document.documentElement).getPropertyValue('--chart-1').trim(),
        )
        await expect(
          page
            .getByRole('img', { name: 'Actividad de tokens' })
            .locator(`path[fill="${chartColor}"]`)
            .first(),
        ).toBeVisible()
      }
      if (section === 'skills') await expect(page.locator('.skill-card').first()).toBeVisible()
      if (section === 'profile')
        await expect(page.getByLabel('Nombre · Solo lectura')).toHaveValue('Ana Example')
      expect(
        await page
          .locator('.settings-page')
          .evaluate((node) => node.scrollWidth <= node.clientWidth),
      ).toBe(true)
      await page.screenshot({
        path: testInfo.outputPath(`${section}-${viewport.width}.png`),
        fullPage: true,
      })
      if (section === 'skills') {
        await page.getByRole('button', { name: /daily-notes Keep/ }).click()
        await page.getByRole('dialog').getByRole('button', { name: 'Editar', exact: true }).click()
        await expect(
          page.getByRole('dialog').getByRole('textbox', { name: 'Editor de SKILL.md' }),
        ).toBeVisible()
        await expect(
          page.getByRole('dialog').getByRole('button', { name: 'Guardar', exact: true }),
        ).toBeInViewport()
        await page.screenshot({ path: testInfo.outputPath(`skill-editor-${viewport.width}.png`) })
        await page.keyboard.press('Escape')
      }

      if (section === 'usage') {
        await page.locator('.settings-page').evaluate((node) => {
          node.scrollTop = node.scrollHeight
        })
        await page.screenshot({ path: testInfo.outputPath(`usage-details-${viewport.width}.png`) })
      }
    }
  }
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.goto('/settings?section=usage')
  const chart = page.getByRole('img', { name: 'Actividad de tokens' })
  await expect(chart.locator('svg')).toBeVisible()
  const before = await chart.locator('svg').innerHTML()
  await page.evaluate(() => document.documentElement.classList.add('dark'))
  await expect.poll(() => chart.locator('svg').innerHTML()).not.toBe(before)
  await page.screenshot({ path: testInfo.outputPath('usage-dark.png') })
})

test('31 categories stay on one row with ranked chips and searchable keyboard overflow', async ({
  page,
}, testInfo) => {
  await mock(page)
  await page.setViewportSize({ width: 1440, height: 900 })
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method !== 'skills.groups') return route.fallback()
    await route.fulfill({
      json: {
        jsonrpc: '2.0',
        id: request.id,
        result: {
          groups: Array.from({ length: 31 }, (_, index) => ({
            id: index === 0 ? 'writing' : `group-${index}`,
            name_en: `Category ${index}`,
            name_es: `Categoría ${index}`,
            description_en: '',
            description_es: '',
            count: index + 1,
          })),
        },
      },
    })
  })
  await page.goto('/settings?section=skills')
  const categories = page.getByRole('group', { name: 'Categorías' })
  await expect(categories.locator('.skill-categories button')).toHaveCount(9)
  await expect(categories.locator('.skill-categories button').nth(1)).toHaveText('Categoría 30 31')
  await expect(categories.getByRole('button', { name: '+23 más' })).toBeVisible()
  await page.screenshot({ path: testInfo.outputPath('categories-1440.png') })
  await categories.getByRole('button', { name: '+23 más' }).click()
  const search = page.getByRole('textbox', { name: 'Buscar categorías' })
  await expect(search).toBeFocused()
  await page.screenshot({ path: testInfo.outputPath('categories-popover-1440.png') })
  await search.fill('no matches')
  await expect(page.getByRole('status')).toContainText('No se encontraron categorías.')
  await search.fill('Categoría 0')
  await search.press('ArrowDown')
  await expect(page.getByRole('button', { name: 'Categoría 0 1', exact: true })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('textbox', { name: 'Buscar categorías' })).toHaveCount(0)
  await expect(categories.getByRole('button', { name: 'Categoría 0', exact: true })).toBeFocused()
  await expect(
    categories.getByRole('button', { name: 'Categoría 0', exact: true }),
  ).toHaveAttribute('data-selected', 'true')
  await expect(page.getByRole('button', { name: /daily-notes Keep/ })).toBeVisible()
  await expect(page.getByRole('button', { name: /review-code Review/ })).toHaveCount(0)
  await categories.getByRole('button', { name: 'Categoría 0', exact: true }).click()
  await page.keyboard.press('Escape')
  await expect(categories.getByRole('button', { name: 'Categoría 0', exact: true })).toBeFocused()
  await page.setViewportSize({ width: 390, height: 844 })
  const heights = await categories
    .locator('.skill-categories button')
    .evaluateAll((buttons) => buttons.map((button) => button.getBoundingClientRect().top))
  expect(new Set(heights).size).toBe(1)
  await page.screenshot({ path: testInfo.outputPath('categories-390.png') })
  expect(
    await page.locator('.settings-page').evaluate((node) => node.scrollWidth <= node.clientWidth),
  ).toBe(true)
})

test('source and status listboxes support keyboard selection and combined filtering', async ({
  page,
}) => {
  await mock(page)
  await page.goto('/settings?section=skills')
  const source = page.getByRole('combobox', { name: 'Origen', exact: true })
  await source.focus()
  await page.keyboard.press('Enter')
  await expect(page.getByRole('option', { name: 'Todos', exact: true })).toBeFocused()
  await page.keyboard.press('End')
  await expect(page.getByRole('option', { name: 'Proyecto', exact: true })).toBeFocused()
  await page.keyboard.press('ArrowUp')
  await expect(page.getByRole('option', { name: 'Instalada', exact: true })).toBeFocused()
  await page.keyboard.press('ArrowUp')
  await expect(page.getByRole('option', { name: 'Usuario', exact: true })).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(source).toContainText('Usuario')
  await expect(source).toBeFocused()
  await expect(page.getByRole('button', { name: /review-code Review/ })).toHaveCount(0)
  await page.getByRole('combobox', { name: 'Estado', exact: true }).click()
  await page.getByRole('option', { name: 'Desactivada', exact: true }).click()
  await expect(page.getByRole('button', { name: /daily-notes Keep/ })).toHaveCount(0)
  await expect(page.locator('.settings-content')).toContainText('Todavía no hay datos.')
})

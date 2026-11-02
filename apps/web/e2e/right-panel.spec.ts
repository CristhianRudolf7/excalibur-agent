import { test, expect } from './fixtures'
import { mockChat } from './chat-fixture'
import { mockEvents } from './event-fixture'
import type { ActivityItem, FileChange } from '../src/lib/rpc/contract'
const change: FileChange = {
  path: 'src/app.ts',
  status: 'modified',
  added: 1,
  removed: 1,
  content: 'const ready = true\n',
  hunks: [
    {
      old_start: 1,
      old_lines: 1,
      new_start: 1,
      new_lines: 1,
      lines: ['-const ready = false', '+const ready = true'],
    },
  ],
}
const item: ActivityItem = {
  id: 'run-1',
  kind: 'subagent',
  name: 'Implement panel',
  profile: 'implementer',
  state: 'running',
  step: 'Editing Shell.tsx',
  progress: { done: 1, total: 3 },
  started_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
}
for (const width of [1440, 390]) {
  test(`right panel launchers, live activity, changes and tree at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 })
    await page.addInitScript(() => {
      localStorage.setItem('excalibur-ui-theme', 'light')
      localStorage.setItem('panel-open', '1')
    })
    const bus = await mockEvents(page)
    await mockChat(page, [
      {
        text: 'Updated the workspace.',
        changes: [
          change,
          { path: 'report.pdf', status: 'added', added: 0, removed: 0, binary: true, hunks: [] },
        ],
      },
    ])
    await page.route('**/rpc', async (route) => {
      const request = route.request().postDataJSON()
      if (request.method !== 'activity.list') return route.fallback()
      expect(request.params.conversation_id).toBe('cnv_web_excalibur')
      await route.fulfill({
        json: {
          jsonrpc: '2.0',
          id: request.id,
          result: {
            items: [
              item,
              { ...item, id: 'child', name: 'Inspect files', kind: 'task', parent_id: item.id },
            ],
          },
        },
      })
    })
    await page.goto('/agents/excalibur?view=chat')
    await expect(page.locator('.right-panel')).toHaveCount(0)
    await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
    await expect(page.getByRole('heading', { name: '¿Qué quieres ver?' })).toBeVisible()
    await expect(page.locator('.panel-launcher')).toHaveCount(4)
    await expect(page.locator('.panel-tabs')).toHaveCount(0)
    await expect(page.locator('.panel-launcher').filter({ hasText: 'Actividad' })).toContainText(
      '2 en curso',
    )
    await page.screenshot({
      path: `/home/cristhian/projects/ui-design/rp-shots/fe-a-home-${width}.png`,
      animations: 'disabled',
    })
    const launch = (name: string) =>
      page.locator('.panel-launcher').filter({ hasText: name }).click()
    const back = () => page.getByRole('button', { name: 'Volver al inicio del panel' }).click()
    await launch('Actividad')
    await expect(page).toHaveURL(/panel=activity/)
    await expect(page.locator('.activity-children')).toContainText('Inspect files')
    await bus.ready('activity.updated')
    bus.emit('activity.updated', {
      item: {
        ...item,
        state: 'completed',
        step: 'Panel ready',
        progress: { done: 3, total: 3 },
        updated_at: new Date(Date.now() + 1000).toISOString(),
        finished_at: new Date().toISOString(),
      },
    })
    await expect(page.getByRole('heading', { name: 'Recientes', exact: true })).toBeVisible()
    await expect(page.locator('.activity-completed')).toContainText('Panel ready')
    await page.screenshot({
      path: `/home/cristhian/projects/ui-design/rp-shots/fe-a-activity-${width}.png`,
      animations: 'disabled',
    })
    await back()
    await launch('Cambios')
    await expect(page).toHaveURL(/panel=changes/)
    await expect(page.locator('.panel-changes .diff-add')).toContainText('const ready = true')
    await expect(page.locator('.panel-changes .diff-del')).toContainText('const ready = false')
    const binary = page.locator('.panel-changes .change-item').filter({ hasText: 'report.pdf' })
    await expect(binary.getByRole('button', { name: 'Abrir report.pdf en Archivos' })).toHaveText(
      'Abrir',
    )
    await expect(binary.locator('.change-diff')).toHaveCount(0)
    await page.screenshot({
      path: `/home/cristhian/projects/ui-design/rp-shots/fe-a-changes-${width}.png`,
      animations: 'disabled',
    })
    await page.locator('.panel-changes .change-path').filter({ hasText: 'app.ts' }).click()
    await expect(page).toHaveURL(/panel=files/)
    await expect(page.locator('.monaco-editor')).toBeVisible()
    await expect(page.locator('.diff-added').first()).toBeVisible()
    const toggle = page.getByRole('button', { name: 'Mostrar u ocultar el explorador' })
    await toggle.click()
    await expect(page.locator('.file-explorer')).toHaveCSS('width', '0px')
    await back()
    await launch('Archivos')
    await expect(page.locator('.file-explorer')).toHaveCSS('width', '0px')
    await toggle.click()
    await expect(page.locator('.tree-row.selected')).toContainText('app.ts')
    await page.screenshot({
      path: `/home/cristhian/projects/ui-design/rp-shots/fe-a-files-${width}.png`,
      animations: 'disabled',
    })
    await back()
    await launch('Pizarra')
    await expect(page).toHaveURL(/panel=whiteboard/)
    await expect(page.locator('.board-empty')).toBeVisible()
    await expect(
      page.locator('.board-empty').getByRole('heading', { name: 'Pizarra' }),
    ).toBeVisible()
    await back()
    await page.evaluate(() => document.documentElement.classList.add('dark'))
    expect(
      await page.locator('.panel-home').evaluate((node) => {
        const probe = document.createElement('div')
        probe.style.background = 'var(--surface)'
        node.append(probe)
        const equal =
          getComputedStyle(node).backgroundColor === getComputedStyle(probe).backgroundColor
        probe.remove()
        return equal
      }),
    ).toBe(true)
    await page.screenshot({
      path: `/home/cristhian/projects/ui-design/rp-shots/fe-a-dark-${width}.png`,
      animations: 'disabled',
    })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await expect(page.locator('.panel-launcher').first()).toHaveCSS('animation-name', 'none')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.reload()
    await expect(page.locator('.right-panel')).toHaveCount(0)
  })
}

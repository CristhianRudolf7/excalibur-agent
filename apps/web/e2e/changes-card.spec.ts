import { test, expect } from './fixtures'
import { mockChat } from './chat-fixture'
import type { FileChange } from '../src/lib/rpc/contract'
const changes: FileChange[] = [
  {
    path: 'src/app.ts',
    status: 'modified',
    added: 2,
    removed: 1,
    content: 'const ready = true\nconst done = true\n',
    hunks: [
      {
        old_start: 1,
        old_lines: 1,
        new_start: 1,
        new_lines: 1,
        lines: ['-const ready = false', '+const ready = true'],
      },
      { old_start: 2, old_lines: 0, new_start: 2, new_lines: 1, lines: ['+const done = true'] },
    ],
  },
  {
    path: 'src/new.ts',
    status: 'added',
    added: 1,
    removed: 0,
    content: 'export const fresh = true\n',
    hunks: [
      {
        old_start: 0,
        old_lines: 0,
        new_start: 1,
        new_lines: 1,
        lines: ['+export const fresh = true'],
      },
    ],
  },
  {
    path: 'src/old.ts',
    status: 'deleted',
    added: 0,
    removed: 1,
    hunks: [
      { old_start: 1, old_lines: 1, new_start: 0, new_lines: 0, lines: ['-const retired = true'] },
    ],
  },
]
for (const width of [1440, 390]) {
  test(`file changes open real diffs at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 })
    await page.addInitScript(() => localStorage.setItem('excalibur-ui-theme', 'light'))
    await mockChat(page, [{ text: 'Updated the workspace.', changes }])
    await page.goto('/agents/excalibur?view=chat')
    await expect(page.getByText('3 archivos modificados')).toBeVisible()
    await page.screenshot({
      path: `test-results/fc-shots/fe-card-${width}.png`,
      animations: 'disabled',
    })
    // The chat shows only a one-line summary; diffs live in the panel.
    await expect(page.locator('.chat-body .changes-list')).toHaveCount(0)
    const openFromPanel = async (path: string) => {
      await page.locator('.changes-line').first().click()
      await expect(page).toHaveURL(/panel=changes/)
      await page.getByRole('button', { name: `Abrir ${path} en Archivos`, exact: true }).click()
    }
    for (const change of changes) {
      await openFromPanel(change.path)
      await expect(page).toHaveURL(/panel=files/)
      await expect(
        page.getByRole('tab', { name: change.path.split('/').at(-1)!, exact: true }),
      ).toHaveAttribute('aria-selected', 'true')
      await expect(page.locator('.monaco-editor')).toBeVisible()
      await expect(
        page.locator(change.status === 'deleted' ? '.diff-removed' : '.diff-added').first(),
      ).toBeVisible()
      await expect(page.locator('.tree-row.selected')).toContainText(change.path.split('/').at(-1)!)
      if (change.status === 'modified') {
        await page.getByRole('button', { name: 'Cambio siguiente', exact: true }).click()
        await expect(page.getByText('Cambio 2 de 2')).toBeVisible()
        await expect(page.locator('.editor-status')).toContainText('2')
        await page.getByRole('button', { name: 'Cambio anterior', exact: true }).click()
        await expect(page.getByText('Cambio 1 de 2')).toBeVisible()
      }
      if (width === 390) await page.getByRole('button', { name: 'Cerrar', exact: true }).click()
    }
    await openFromPanel('src/app.ts')
    await page.screenshot({
      path: `test-results/fc-shots/fe-diff-${width}.png`,
      animations: 'disabled',
    })
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await page.evaluate(() => document.documentElement.classList.add('dark'))
    await expect(page.locator('.diff-added').first()).toHaveCSS(
      'background-color',
      'rgb(18, 46, 39)',
    )
    await page.screenshot({
      path: `test-results/fc-shots/fe-dark-${width}.png`,
      animations: 'disabled',
    })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    if (width === 390) await page.getByRole('button', { name: 'Cerrar', exact: true }).click()
    await expect(page.locator('.changes-line').first()).toBeVisible()
  })
}
test('missing content shows hunks and a partial preview notice', async ({ page }) => {
  await mockChat(page, [
    { text: 'Large update.', changes: [{ ...changes[0]!, content: undefined, truncated: true }] },
  ])
  await page.goto('/agents/excalibur?view=chat')
  await page.locator('.changes-line').first().click()
  await page.getByRole('button', { name: 'Abrir src/app.ts en Archivos', exact: true }).click()
  await expect(
    page.getByText('El contenido es demasiado grande. Revisa los cambios disponibles.'),
  ).toBeVisible()
  await expect(page.locator('.change-preview')).toContainText('+const ready = true')
  await expect(page.locator('.files-panel').getByText('Vista parcial de los cambios')).toBeVisible()
})

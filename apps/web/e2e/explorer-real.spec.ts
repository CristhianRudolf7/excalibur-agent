import { test, expect } from './fixtures'
import { mockChat } from './chat-fixture'
import { readFileSync } from 'node:fs'
import { seedDocument, seedPages } from '../src/features/viewer/seeds'

const root = '/workspace/research'
const digest = 'a'.repeat(64)
const document = {
  ...seedDocument('demo-pdf'),
  id: 'report',
  title: 'Project report',
  blob_digest: digest,
}
for (const width of [1440, 390]) {
  test(`real explorer opens code, sheets and PDFs at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: width === 390 ? 844 : 900 })
    await page.addInitScript(() => {
      localStorage.setItem('excalibur-ui-theme', 'light')
    })
    await mockChat(page, [
      {
        text: 'Updated Python',
        changes: [{ path: 'src/main.py', status: 'modified', added: 1, removed: 0, hunks: [] }],
      },
    ])
    const lists: string[] = []
    const opens: string[] = []
    await page.route('**/rpc', async (route) => {
      const { id, method, params } = route.request().postDataJSON()
      let result: unknown
      if (method === 'project.list')
        result = {
          projects: [
            { id: 'research', name: 'Research', roots: [root], status: 'active' },
            { id: 'other', name: 'Other project', roots: ['/workspace/other'], status: 'active' },
          ],
        }
      else if (method === 'files.list') {
        lists.push(`${params.project_id}:${params.path}`)
        result = {
          entries:
            params.project_id === 'other'
              ? ['other.txt']
              : params.path === 'src'
                ? ['src/main.py']
                : ['src/', 'data.csv', 'informe.pdf'],
          next_cursor: '',
        }
      } else if (method === 'files.open') {
        opens.push(params.path)
        expect(params.path.startsWith(`${root}/`)).toBe(true)
        result = {
          path: params.path,
          size: 100,
          mime: 'application/octet-stream',
          ...(params.path.endsWith('.py')
            ? { kind: 'text', content: 'print("real_project_python")' }
            : params.path.endsWith('.csv')
              ? { kind: 'sheet', document_id: 'sheet' }
              : { kind: 'document', document_id: 'report' }),
        }
      } else if (method === 'library.get')
        result = { document: params.id === 'sheet' ? seedDocument('demo-xlsx') : document }
      else if (method === 'library.pages')
        result = { document, pages: seedPages(params.id === 'sheet' ? 'demo-xlsx' : 'demo-pdf') }
      else return route.fallback()
      await route.fulfill({ json: { jsonrpc: '2.0', id, result } })
    })
    await page.route(`**/blobs/${digest}`, (route) =>
      route.fulfill({
        body: readFileSync('e2e/fixtures/report.pdf'),
        contentType: 'application/pdf',
      }),
    )
    await page.goto('/agents/excalibur?view=chat')
    await page.getByRole('button', { name: 'Alternar panel', exact: true }).click()
    const files = async () => {
      await page.locator('.panel-launcher').filter({ hasText: 'Archivos' }).click()
      await expect(page.locator('.project-explorer')).toBeVisible()
    }
    const back = () => page.getByRole('button', { name: 'Volver al inicio del panel' }).click()
    await files()
    await expect(page.locator('.project-explorer')).toContainText('informe.pdf')
    // StrictMode may restart the root request; no child may load before expansion.
    expect([...new Set(lists)]).toEqual(['research:'])
    await expect(page.locator('.project-explorer')).not.toContainText('auth.ts')
    await page.getByRole('combobox', { name: 'Proyecto activo' }).click()
    await page.getByRole('option', { name: 'Other project' }).click()
    await expect(page.locator('.project-explorer')).toContainText('other.txt')
    await page.getByRole('combobox', { name: 'Proyecto activo' }).click()
    await page.getByRole('option', { name: 'Research' }).click()
    await page
      .locator('.project-explorer')
      .getByRole('button', { name: 'src', exact: true })
      .click()
    const python = page
      .locator('.project-explorer')
      .getByRole('button', { name: 'main.py', exact: true })
    await expect(python.locator('.changed-dot')).toBeVisible()
    expect(lists).toContain('research:src')
    await python.click()
    await expect(page.locator('.monaco-editor')).toContainText('real_project_python')
    await expect(page).toHaveURL(/panel=files/)
    await expect(python).toHaveClass(/selected/)
    await page.screenshot({
      path: `/home/cristhian/projects/ui-design/rp-shots/ex-files-${width}.png`,
      animations: 'disabled',
    })
    if (width === 1440) {
      await page.evaluate(() => document.documentElement.classList.add('dark'))
      await page.screenshot({
        path: '/home/cristhian/projects/ui-design/rp-shots/ex-files-dark.png',
        animations: 'disabled',
      })
      await page.evaluate(() => document.documentElement.classList.remove('dark'))
    }
    await page
      .locator('.project-explorer')
      .getByRole('button', { name: 'data.csv', exact: true })
      .click()
    await expect(page).toHaveURL(/panel=sheet/)
    await expect(page.getByRole('gridcell', { name: /^(Cell|Celda) B2$/ })).toHaveText('42')
    await back()
    await files()
    await page
      .locator('.project-explorer')
      .getByRole('button', { name: 'informe.pdf', exact: true })
      .click()
    await expect(page).toHaveURL(/panel=document/)
    const canvas = page.locator('.viewer-pdf canvas')
    await expect(canvas).toBeVisible()
    await expect
      .poll(() => canvas.evaluate((node: HTMLCanvasElement) => node.width))
      .toBeGreaterThan(100)
    await page.screenshot({
      path: `/home/cristhian/projects/ui-design/rp-shots/ex-pdf-${width}.png`,
      animations: 'disabled',
    })
    expect(opens).toEqual([`${root}/src/main.py`, `${root}/data.csv`, `${root}/informe.pdf`])
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
  })
}

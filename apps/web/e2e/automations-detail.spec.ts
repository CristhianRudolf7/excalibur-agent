import { test, expect } from './fixtures'
import { mockAutomationDetail } from './fixtures/automations-detail'

// A dev-only standalone mount can exercise the same page before the FE1 route merges.
const target = process.env.AUTOMATION_DETAIL_TEST_URL ?? '/automations/daily-digest'

test('diagram, node details, run replay and versions', async ({ page }) => {
  await mockAutomationDetail(page)
  await page.goto(target)
  await expect(page.getByRole('heading', { name: 'Daily project digest' })).toBeVisible()
  await expect(page.locator('.auto-node')).toHaveCount(6)
  await expect(page.getByText('Yes', { exact: true })).toBeVisible()
  await expect(page.getByText('No', { exact: true })).toBeVisible()
  await page.locator('[data-step-id="s1"]').click()
  await expect(
    page
      .locator('.auto-detail-field')
      .getByText('Read the latest project notes and identify the most important changes.'),
  ).toBeVisible()
  await page.getByRole('tab', { name: 'Runs', exact: true }).click()
  await page.getByRole('button', { name: /Scheduled/ }).click()
  await expect(page.locator('[data-step-id="s1"]')).toHaveClass(/auto-step-succeeded/)
  await expect(page.locator('[data-step-id="s4"]')).toHaveClass(/auto-step-skipped/)
  await expect(page.locator('.auto-edge-traversed')).toHaveCount(4)
  await page.locator('[data-step-id="s1"]').click()
  await expect(page.getByText('Three important changes found.', { exact: true })).toBeVisible()
  await page.locator('[data-step-id="s3"]').click()
  await expect(page.locator('.auto-code')).toContainText('import json')
  await page.locator('[data-step-id="s4"]').click()
  await expect(page.locator('.auto-detail-field pre')).toContainText('excalibur')
  await page.getByRole('tab', { name: 'Versions', exact: true }).click()
  await expect(page.getByText('First approved version.')).toBeVisible()
})

test('live run events progressively update nodes', async ({ page }) => {
  const state = await mockAutomationDetail(page)
  state.runs[0] = {
    ...state.runs[0]!,
    status: 'running',
    finished_at: undefined,
    steps: [{ step_id: 's1', status: 'running' }],
  }
  await page.goto(target)
  await expect(page.locator('[data-step-id="s1"]')).toHaveClass(/auto-step-running/)
  await state.events.ready('automation.run')
  state.runs[0]!.steps = [
    { step_id: 's1', status: 'succeeded' },
    { step_id: 's2', status: 'running' },
  ]
  state.events.emit('automation.run', { run: state.runs[0] })
  await expect(page.locator('[data-step-id="s1"]')).toHaveClass(/auto-step-succeeded/)
  await expect(page.locator('[data-step-id="s2"]')).toHaveClass(/auto-step-running/)
  await page.getByRole('tab', { name: 'Runs', exact: true }).click()
  await expect(page.getByText('Live', { exact: true })).toBeVisible()
  state.runs[0] = {
    ...state.runs[0]!,
    status: 'failed',
    error: 'Condition could not be evaluated',
    steps: [
      { step_id: 's1', status: 'succeeded' },
      { step_id: 's2', status: 'failed', error: 'Condition could not be evaluated' },
    ],
  }
  state.events.emit('automation.run', { run: state.runs[0] })
  await expect(page.locator('[data-step-id="s2"]')).toHaveClass(/auto-step-failed/)
  await page.locator('[data-step-id="s2"]').click()
  await expect(page.getByText('Condition could not be evaluated', { exact: true })).toBeVisible()
})

test('zoom and spatial keyboard navigation', async ({ page }) => {
  await mockAutomationDetail(page)
  await page.goto(target)
  const stage = page.locator('.auto-flow-stage')
  const initial = await stage.evaluate((element) => element.style.transform)
  await page.getByRole('button', { name: 'Zoom in', exact: true }).click()
  expect(await stage.evaluate((element) => element.style.transform)).not.toBe(initial)
  await page.getByRole('button', { name: 'Zoom out', exact: true }).click()
  await page.getByRole('button', { name: 'Fit', exact: true }).click()
  expect(await stage.evaluate((element) => element.style.transform)).toBe(initial)
  await page.locator('[data-step-id="__schedule"]').focus()
  await page.keyboard.press('ArrowDown')
  await expect(page.locator('[data-step-id="s1"]')).toBeFocused()
  await page.keyboard.press('Enter')
  await expect(page.locator('[data-step-id="s1"]')).toHaveAttribute('aria-pressed', 'true')
})

for (const mode of ['mobile', 'dark', 'reduced'] as const) {
  test(`${mode} presentation remains usable`, async ({ page }) => {
    await mockAutomationDetail(page)
    await page.setViewportSize(
      mode === 'mobile' ? { width: 390, height: 844 } : { width: 1440, height: 900 },
    )
    if (mode === 'dark')
      await page.addInitScript(() => localStorage.setItem('excalibur-ui-theme', 'dark'))
    if (mode === 'reduced') await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto(target)
    if (mode === 'dark') await page.evaluate(() => document.documentElement.classList.add('dark'))
    await expect(page.locator('.auto-node')).toHaveCount(6)
    await page.locator('[data-step-id="s1"]').click()
    await page.locator('.auto-panel-title').scrollIntoViewIfNeeded()
    await expect(page.locator('.auto-panel-title')).toBeVisible()
    await expect(page.locator('.auto-panel-title')).toContainText('Review the project')
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    if (mode === 'dark') {
      await expect(page.locator('html')).toHaveClass(/dark/)
      expect(
        await page
          .locator('.auto-node')
          .first()
          .evaluate((element) => getComputedStyle(element).backgroundColor),
      ).not.toBe('rgb(255, 255, 255)')
    }
    if (mode === 'reduced')
      expect(
        await page
          .locator('.auto-node')
          .first()
          .evaluate((element) => getComputedStyle(element).animationName),
      ).toBe('none')
  })
}

test('not-found and retry states', async ({ page }) => {
  const state = await mockAutomationDetail(page)
  state.missing = true
  await page.goto(target)
  await expect(page.getByRole('alert')).toContainText('Automation not found.')
  await expect(page.getByRole('link', { name: 'Automations' })).toHaveAttribute(
    'href',
    '/automations',
  )
  state.missing = false
  state.fail = true
  await page.reload()
  await expect(page.getByRole('button', { name: 'Retry', exact: true })).toBeVisible()
  state.fail = false
  await page.getByRole('button', { name: 'Retry', exact: true }).click()
  await expect(page.getByRole('heading', { name: 'Daily project digest' })).toBeVisible()
})

import { test, expect } from './fixtures'
import { mockChat } from './chat-fixture'

// 16x16 blue PNG standing in for site favicons.
const png = Buffer.from(
  'iVBORw0KGgoAAAANSUhEUgAAABAAAAAQCAIAAACQkWg2AAAAGUlEQVR4nGPUyDvBQApgIkn1qIZRDUNKAwBg0AF+aXHXEwAAAABJRU5ErkJggg==',
  'base64',
)

test.use({ viewport: { width: 390, height: 844 } })

test('source favicons sit inside their circles and link icons stay small and inline', async ({
  page,
}, testInfo) => {
  const domains = ['elperiodico.com', 'elespanol.com', 'infobae.com']
  await mockChat(page, [
    { role: 'user', text: 'Noticias de IA' },
    {
      role: 'assistant',
      text: '- **CENID propone un sistema de IA para anticipar inundaciones y mejorar la respuesta ante emergencias.** [El Español](https://elespanol.com/a)\n- Un ejecutivo afirma gastar mucho en una IA, informa *Infobae*. [Infobae](https://infobae.com/b)',
      sources: domains.map((domain) => ({
        kind: 'web',
        title: domain,
        url: `https://${domain}/nota`,
        domain,
      })),
    },
  ])
  await page.route('**/favicon.ico', (route) =>
    route.fulfill({ contentType: 'image/png', body: png }),
  )
  await page.goto('/agents/excalibur?view=chat')
  const chip = page.getByRole('button', { name: /Fuentes \(3\)/ })
  await expect(chip).toBeVisible()
  const circles = chip.locator('.source-icon')
  await expect(circles).toHaveCount(3)
  for (let index = 0; index < 3; index++) {
    const circle = (await circles.nth(index).boundingBox())!
    // Remote favicons do not load in CI: the domain initial fills the circle instead.
    const glyph = circles.nth(index).locator('img, .source-initial').first()
    await expect(glyph).toBeVisible()
    const icon = (await glyph.boundingBox())!
    expect(icon.y).toBeGreaterThanOrEqual(circle.y - 0.5)
    expect(icon.y + icon.height).toBeLessThanOrEqual(circle.y + circle.height + 0.5)
  }
  const linkIcon = (await page
    .locator('.markdown a .markdown-link-icon svg')
    .first()
    .boundingBox())!
  expect(linkIcon.height).toBeLessThanOrEqual(12)
  // The icon shares the line with the end of its link text.
  const link = page.locator('.markdown a').first()
  const rects = await link.evaluate((node) =>
    Array.from(node.getClientRects()).map((rect) => rect.bottom),
  )
  expect(Math.abs(linkIcon.y + linkIcon.height - rects.at(-1)!)).toBeLessThan(14)
  await page.screenshot({ path: testInfo.outputPath('sources-chip-390.png') })
})

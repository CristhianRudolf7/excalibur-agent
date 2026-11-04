import { test, expect } from './fixtures'
import { mockChat } from './chat-fixture'

const url = 'https://a-small-site-for-opening-on-your-phone.trycloudflare.com'
const expires = '2027-01-01T12:00:00Z'

for (const language of ['es', 'en']) {
  for (const theme of ['light', 'dark']) {
    test(`public preview history card at 390 px in ${language}, ${theme}`, async ({
      page,
    }, testInfo) => {
      await page.setViewportSize({ width: 390, height: 844 })
      await page.addInitScript(
        ({ language, theme }) => {
          localStorage.setItem('excalibur-ui-language', language)
          localStorage.setItem('excalibur-ui-theme', theme)
          Object.defineProperty(navigator, 'clipboard', {
            configurable: true,
            value: {
              writeText: async (text: string) => localStorage.setItem('preview-copy', text),
            },
          })
        },
        { language, theme },
      )
      await mockChat(page, [
        {
          role: 'assistant',
          text: `Done.\n\n[Public preview](${url} "public-preview:${expires}")`,
        },
      ])
      await page.goto('/agents/excalibur?view=chat')
      const card = page.getByRole('group', {
        name: language === 'es' ? 'Vista previa pública' : 'Public preview',
      })
      await expect(card).toBeVisible()
      const link = card.getByRole('link')
      await expect(link).toHaveAttribute('href', url)
      await expect(link).toHaveAttribute('target', '_blank')
      await expect(card.locator('time')).toHaveAttribute('datetime', expires)
      await card
        .getByRole('button', {
          name: language === 'es' ? 'Copiar enlace público' : 'Copy public link',
        })
        .click()
      await expect(card.getByRole('status')).toHaveText(
        language === 'es' ? 'Enlace copiado' : 'Link copied',
      )
      expect(await page.evaluate(() => localStorage.getItem('preview-copy'))).toBe(url)
      expect(await card.evaluate((node) => node.scrollWidth <= node.clientWidth)).toBe(true)
      const box = (await card.boundingBox())!
      expect(box.x).toBeGreaterThanOrEqual(0)
      expect(box.x + box.width).toBeLessThanOrEqual(390)
      await page.screenshot({
        path: testInfo.outputPath(`public-preview-${language}-${theme}-390.png`),
      })
      await page.reload()
      await expect(card).toBeVisible()
    })
  }
}

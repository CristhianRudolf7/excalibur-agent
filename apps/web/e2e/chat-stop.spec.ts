import { test, expect } from './fixtures'
import { mockChat } from './chat-fixture'

test('the stop control message is never shown as something the user wrote', async ({ page }) => {
  await mockChat(page, [
    { role: 'user', text: 'Cuéntame una historia larga' },
    { role: 'user', text: '/stop' },
    {
      role: 'assistant',
      text: 'Detuve la respuesta en esta conversación. Las tareas en segundo plano siguen activas.',
    },
  ])
  await page.goto('/agents/excalibur?view=chat')
  await expect(page.getByText('Cuéntame una historia larga')).toBeVisible()
  await expect(page.getByText('Detuve la respuesta en esta conversación.')).toBeVisible()
  await expect(page.locator('.message-user')).toHaveCount(1)
  await expect(page.getByText('/stop', { exact: true })).toHaveCount(0)
})

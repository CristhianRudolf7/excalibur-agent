import { test, expect } from '@playwright/test'

test('profile is read from the real daemon over the Vite proxy', async ({ page }) => {
  await page.goto('/agents/excalibur?view=chat')
  const profile = page.locator('.sidebar').getByRole('button', { name: 'Perfil', exact: true })
  await expect(profile).toContainText('Ana Prueba')
  await expect(profile).toContainText('AP')
  await profile.click()
  const dialog = page.getByRole('dialog', { name: 'Perfil', exact: true })
  await expect(dialog.getByText('Ana Prueba', { exact: true })).toBeVisible()
  await expect(dialog.getByText('AP', { exact: true })).toBeVisible()
  await page.keyboard.press('Escape')
  await page.keyboard.press('Control+b')
  const railProfile = page
    .getByRole('complementary', { name: 'Navegación compacta' })
    .getByRole('button', { name: 'Perfil', exact: true })
  await expect(railProfile).toHaveText('AP')
  await railProfile.click()
  await expect(dialog.getByText('Ana Prueba', { exact: true })).toBeVisible()
})

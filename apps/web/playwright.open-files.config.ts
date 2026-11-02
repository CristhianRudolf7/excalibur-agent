import { defineConfig } from '@playwright/test'
import base from './playwright.config'
const port = Number(process.env.EXCALIBUR_WEB_TEST_PORT ?? 4472)
export default defineConfig({
  ...base,
  use: { ...base.use, baseURL: `http://127.0.0.1:${port}` },
  timeout: 120_000,
  expect: { timeout: 30_000 },
  testIgnore: /e2e[\\/]integration[\\/]/,
  webServer: {
    command: `pnpm dev --host 127.0.0.1 --port ${port}`,
    url: `http://127.0.0.1:${port}`,
    reuseExistingServer: false,
    timeout: 180_000,
  },
})

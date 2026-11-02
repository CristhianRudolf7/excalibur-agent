import { defineConfig } from '@playwright/test'

const port = Number(process.env.EXCALIBUR_WEB_TEST_PORT ?? process.env.EXCALIBUR_E2E_PORT ?? 4183)

export default defineConfig({
  testDir: './e2e',
  timeout: 60_000,
  expect: { timeout: 10_000 },
  // Match only e2e/integration; a glob like **/integration/** would also
  // match a checkout whose own path contains an "integration" directory.
  // Source-module harnesses run with playwright.open-files.config.ts.
  testIgnore: [/e2e[\\/]integration[\\/]/, /open-files\.spec\.ts$/],
  // Uses the system Chrome; no separate Playwright browser download.
  use: { baseURL: `http://127.0.0.1:${port}`, channel: 'chrome' },
  webServer: {
    timeout: 180_000,
    command: `pnpm build && pnpm preview --host 127.0.0.1 --strictPort --port ${port}`,
    url: `http://127.0.0.1:${port}`,
    reuseExistingServer: false,
  },
})

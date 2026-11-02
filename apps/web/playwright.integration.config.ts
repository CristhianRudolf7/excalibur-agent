import { defineConfig } from '@playwright/test'

export default defineConfig({
  testDir: './e2e/integration',
  workers: 1,
  use: { baseURL: 'http://127.0.0.1:4184', channel: 'chrome' },
  webServer: [
    {
      command: 'bash scripts/dev-daemon.sh',
      env: {
        EXCALIBUR_USER_NAME: 'Ana Prueba',
        EXCALIBUR_DEV_LISTEN: '127.0.0.1:7788',
        EXCALIBUR_PROVIDER: 'stub',
        EXCALIBUR_PRINCIPAL_ENABLED: '1',
      },
      url: 'http://127.0.0.1:7788/healthz',
      timeout: 120_000,
      reuseExistingServer: false,
      gracefulShutdown: { signal: 'SIGTERM', timeout: 10_000 },
    },
    {
      command: 'pnpm build && pnpm preview --host 127.0.0.1 --port 4184',
      env: { EXCALIBUR_DAEMON_URL: 'http://127.0.0.1:7788' },
      url: 'http://127.0.0.1:4184',
      reuseExistingServer: false,
    },
  ],
})

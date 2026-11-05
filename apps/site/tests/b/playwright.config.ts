import { defineConfig } from '@playwright/test'
import { baseURL } from '../server'
export default defineConfig({
  testDir: '.',
  testMatch: '**/*.spec.ts',
  workers: 1,
  fullyParallel: false,
  reporter: 'list',
  use: { baseURL, channel: 'chrome', headless: true, trace: 'retain-on-failure' },
})

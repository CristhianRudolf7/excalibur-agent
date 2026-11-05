import { defineConfig } from '@playwright/test'
import { baseURL } from './tests/server'
export default defineConfig({
  testDir: './tests',
  testMatch: '**/*.spec.ts',
  fullyParallel: false,
  workers: 1,
  globalSetup: './tests/setup.ts',
  reporter: 'list',
  use: {
    baseURL,
    channel: 'chrome',
    headless: true,
    trace: 'retain-on-failure',
  },
})

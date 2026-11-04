import { createRequire } from 'node:module'
import { isStopping, spawnManaged, waitForChild } from './processes.mjs'

const require = createRequire(import.meta.url)
const child = spawnManaged(
  process.execPath,
  [require.resolve('@playwright/test/cli'), 'test', ...process.argv.slice(2)],
  { env: { ...process.env, EXCALIBUR_WEB_ROBOTLAB_TEST_TRACKER: '1' } },
)
const code = await waitForChild(child)
if (!isStopping()) process.exitCode = code

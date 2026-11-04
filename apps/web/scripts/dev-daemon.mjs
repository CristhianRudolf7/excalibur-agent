import { mkdtempSync, rmSync } from 'node:fs'
import { delimiter, join } from 'node:path'
import { homedir, tmpdir } from 'node:os'
import { fileURLToPath } from 'node:url'
import { isStopping, spawnManaged, waitForChild } from './processes.mjs'

const windows = process.platform === 'win32'
const repo = fileURLToPath(new URL('../../../', import.meta.url))
const state = mkdtempSync(join(tmpdir(), 'excalibur-web-daemon.'))
const binary = join(state, windows ? 'excalibur.exe' : 'excalibur')
const env = {
  ...process.env,
  PATH: [
    ...(windows ? [] : ['/usr/local/go/bin']),
    ...(process.platform === 'darwin' ? ['/opt/homebrew/bin', '/usr/local/bin'] : []),
    join(homedir(), 'go', 'bin'),
    process.env.PATH ?? '',
  ].join(delimiter),
}
// Register after the supervisor's exit handler so children release files first.
process.on('exit', () => rmSync(state, { recursive: true, force: true, maxRetries: 5 }))

try {
  const build = ['go', '-C', repo, 'build', '-p', '2', '-o', binary, './cmd/excalibur']
  const child = windows
    ? spawnManaged(build[0], build.slice(1), { env })
    : spawnManaged('nice', ['-n', '19', ...build], { env })
  const code = await waitForChild(child)
  if (!isStopping()) {
    if (code !== 0) process.exitCode = code
    else {
      const daemon = spawnManaged(
        binary,
        [
          'daemon',
          'start',
          '--listen',
          process.env.EXCALIBUR_DEV_LISTEN || '127.0.0.1:7787',
          '--data-dir',
          join(state, 'data'),
        ],
        { env },
      )
      const daemonCode = await waitForChild(daemon)
      if (!isStopping()) process.exitCode = daemonCode
    }
  }
} catch (error) {
  console.error(error.message)
  process.exitCode = 1
}

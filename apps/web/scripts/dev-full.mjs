import { fileURLToPath } from 'node:url'
import { shutdown, spawnManaged } from './processes.mjs'

if (process.platform === 'darwin') {
  process.env.PATH = `/opt/homebrew/bin:/usr/local/bin:/usr/local/go/bin:${process.env.PATH ?? ''}`
}

const cwd = fileURLToPath(new URL('../', import.meta.url))
const children = [
  spawnManaged(process.execPath, [fileURLToPath(new URL('./dev-daemon.mjs', import.meta.url))], {
    cwd,
    stdio: ['inherit', 'inherit', 'inherit', 'ipc'],
  }),
  spawnManaged('pnpm', ['dev'], { cwd, shell: process.platform === 'win32' }),
]
for (const child of children) {
  child.once('close', (code) => shutdown(code ?? 1))
}

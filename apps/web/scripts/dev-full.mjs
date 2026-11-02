import { spawn } from 'node:child_process'

const grouped = process.platform !== 'win32'
const children = [
  spawn('bash', ['scripts/dev-daemon.sh'], { stdio: 'inherit', detached: grouped }),
  spawn('pnpm', ['dev'], { stdio: 'inherit', detached: grouped }),
]
let stopping = false
function stop(code) {
  if (stopping) return
  stopping = true
  process.exitCode = code
  for (const child of children) {
    if (!child.pid) continue
    try {
      if (grouped) process.kill(-child.pid, 'SIGTERM')
      else child.kill('SIGTERM')
    } catch (error) {
      if (error.code !== 'ESRCH') process.exitCode = 1
    }
  }
}
process.on('SIGINT', () => stop(130))
process.on('SIGTERM', () => stop(143))
for (const child of children) {
  child.on('error', () => stop(1))
  child.on('exit', (code) => stop(code ?? 1))
}

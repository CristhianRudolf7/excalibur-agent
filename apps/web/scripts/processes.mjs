import { spawn, spawnSync } from 'node:child_process'

const windows = process.platform === 'win32'
const children = new Set()
const stoppingGroups = new Set()
let stopping = false

function killTree(child, signal) {
  if (!child.pid) return
  if (windows) {
    // A shell-spawned .cmd has descendants that child.kill() does not terminate.
    spawnSync('taskkill', ['/pid', String(child.pid), '/T', '/F'], { stdio: 'ignore' })
    return
  }
  try {
    process.kill(-child.pid, signal)
  } catch (error) {
    if (error.code !== 'ESRCH') process.exitCode = 1
  }
}

export function isStopping() {
  return stopping
}

export function shutdown(code = 0) {
  if (stopping) return
  stopping = true
  process.exitCode = code
  for (const child of children) {
    if (!child.pid) continue
    stoppingGroups.add(child)
    const supervised = child.connected
    if (supervised) {
      // Let a Node supervisor stop its own groups and remove temporary state.
      child.send({ type: 'excalibur:shutdown', code }, (error) => {
        if (error) killTree(child, 'SIGTERM')
      })
    } else killTree(child, 'SIGTERM')
    if (supervised || !windows) {
      // Nested supervisors need longer than their own two-second kill grace.
      setTimeout(
        () => {
          if (!supervised || (child.exitCode === null && child.signalCode === null)) {
            killTree(child, 'SIGKILL')
          }
          stoppingGroups.delete(child)
        },
        supervised ? 4_000 : 2_000,
      )
    } else stoppingGroups.delete(child)
  }
}

export function spawnManaged(command, args, options = {}) {
  if (stopping) throw new Error('Process supervisor is stopping')
  const child = spawn(command, args, {
    stdio: 'inherit',
    detached: !windows,
    ...options,
  })
  children.add(child)
  child.once('close', () => children.delete(child))
  child.once('error', (error) => {
    console.error(`Could not start ${command}: ${error.message}`)
    shutdown(1)
  })
  return child
}

export function waitForChild(child) {
  return new Promise((resolve) => {
    child.once('close', (code) => resolve(code ?? 1))
  })
}

process.on('message', (message) => {
  if (message?.type === 'excalibur:shutdown' && Number.isInteger(message.code)) {
    shutdown(message.code)
  }
})
// An idle IPC channel must not keep a completed daemon launcher alive.
process.channel?.unref()
process.on('SIGINT', () => shutdown(130))
process.on('SIGTERM', () => shutdown(143))
process.on('exit', () => {
  for (const child of new Set([...children, ...stoppingGroups])) killTree(child, 'SIGKILL')
})

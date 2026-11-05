import { spawn } from 'node:child_process'
import { mkdir, writeFile, rm, readFile } from 'node:fs/promises'
import { setTimeout as delay } from 'node:timers/promises'

export const port = process.env.SITE_PORT ?? '4336'
export const baseURL = `http://127.0.0.1:${port}`
/** Reuse a preview only when one already owns this port. Stop only our recorded PID. */
export async function startPreview(): Promise<() => Promise<void>> {
  try {
    const response = await fetch(baseURL, { signal: AbortSignal.timeout(700) })
    if (response.ok) {
      const current = await readFile('dist/index.html', 'utf8')
      if ((await response.text()) !== current) {
        process.env.SITE_FROM_DISK = '1'
        console.warn(
          `The existing ${baseURL} serves a different build; Chrome will route this checkout's dist without stopping that preview.`,
        )
      }
      return async () => {}
    }
  } catch {
    /* Start our production preview. */
  }
  const child = spawn(
    process.execPath,
    [
      'node_modules/astro/bin/astro.mjs',
      'preview',
      '--host',
      '127.0.0.1',
      '--port',
      port,
      '--ignore-lock',
    ],
    { stdio: 'ignore' },
  )
  await mkdir('test-results', { recursive: true })
  await writeFile(`test-results/preview-${port}.pid`, String(child.pid))
  const stop = async () => {
    if (child.exitCode === null) {
      await new Promise<void>((resolve) => {
        child.once('exit', () => resolve())
        child.kill('SIGTERM')
      })
    }
    await rm(`test-results/preview-${port}.pid`, { force: true })
  }
  for (let attempt = 0; attempt < 80; attempt++) {
    if (child.exitCode !== null) throw new Error(`Preview exited with ${child.exitCode}`)
    try {
      if ((await fetch(baseURL)).ok) return stop
    } catch {
      /* Await readiness. */
    }
    await delay(100)
  }
  await stop()
  throw new Error(`Production preview did not start on ${baseURL}`)
}

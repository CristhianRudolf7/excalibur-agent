import assert from 'node:assert/strict'
import { spawn } from 'node:child_process'
import { once } from 'node:events'
import { mkdtemp, readFile, rm } from 'node:fs/promises'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { setTimeout as delay } from 'node:timers/promises'
import test from 'node:test'

const supervisor = new URL('./processes.mjs', import.meta.url).href

function run(source) {
  return spawn(process.execPath, ['--input-type=module', '-e', source], {
    stdio: ['ignore', 'pipe', 'pipe'],
  })
}

test('returns child failures without changing their exit code', { timeout: 10_000 }, async () => {
  const runner = run(`
    import { spawnManaged, waitForChild } from ${JSON.stringify(supervisor)};
    process.exitCode = await waitForChild(spawnManaged(process.execPath, ['-e', 'process.exit(7)']));
  `)
  const [code] = await once(runner, 'close')
  assert.equal(code, 7)
})

test('reports a missing command and exits unsuccessfully', { timeout: 10_000 }, async () => {
  const runner = run(`
    import { spawnManaged, waitForChild } from ${JSON.stringify(supervisor)};
    await waitForChild(spawnManaged('excalibur-nonexistent-test-command', []));
  `)
  let stderr = ''
  runner.stderr.on('data', (data) => (stderr += data))
  const [code] = await once(runner, 'close')
  assert.equal(code, 1)
  assert.match(stderr, /Could not start excalibur-nonexistent-test-command/)
})

test(
  'shutdown terminates a child tree even when it ignores SIGTERM',
  { timeout: 15_000 },
  async () => {
    const state = await mkdtemp(join(tmpdir(), 'excalibur-process-test.'))
    const marker = join(state, 'ticks')
    const grandchild = `
    const fs = require('node:fs');
    process.on('SIGTERM', () => {});
    setInterval(() => fs.appendFileSync(${JSON.stringify(marker)}, '.'), 30);
  `
    const child = `
    const { spawn } = require('node:child_process');
    process.on('SIGTERM', () => {});
    spawn(process.execPath, ['-e', ${JSON.stringify(grandchild)}], { stdio: 'inherit' });
    setInterval(() => {}, 1000);
  `
    // Use stdin so Windows exercises taskkill without relying on Unix signals.
    const active = spawn(
      process.execPath,
      [
        '--input-type=module',
        '-e',
        `
    import { spawnManaged, shutdown } from ${JSON.stringify(supervisor)};
    spawnManaged(process.execPath, ['-e', ${JSON.stringify(child)}]);
    process.stdin.resume();
    process.stdin.once('data', () => { process.stdin.pause(); shutdown(143); });
  `,
      ],
      { stdio: ['pipe', 'pipe', 'pipe'] },
    )
    const closed = once(active, 'close')
    try {
      let ready = false
      for (let attempt = 0; attempt < 100; attempt++) {
        try {
          ready = (await readFile(marker, 'utf8')).length > 0
        } catch (error) {
          if (error.code !== 'ENOENT') throw error
        }
        if (ready) break
        await delay(30)
      }
      assert.ok(ready, 'grandchild started')
      active.stdin.end('stop')
      const [code] = await closed
      assert.equal(code, 143)
      const contents = await readFile(marker, 'utf8')
      await delay(150)
      assert.equal(await readFile(marker, 'utf8'), contents, 'grandchild has stopped writing')
    } finally {
      if (active.exitCode === null) {
        active.stdin.end('stop')
        await closed
      }
      await rm(state, { recursive: true, force: true })
    }
  },
)

test(
  'nested supervisors finish exit cleanup before the parent forces shutdown',
  { timeout: 20_000 },
  async () => {
    const state = await mkdtemp(join(tmpdir(), 'excalibur-supervisor-test.'))
    const marker = join(state, 'cleaned')
    const child = `
    import { spawnManaged } from ${JSON.stringify(supervisor)};
    import { writeFileSync } from 'node:fs';
    process.on('exit', () => writeFileSync(${JSON.stringify(marker)}, 'cleaned'));
    spawnManaged(process.execPath, ['-e', 'setInterval(() => {}, 1000)']);
    process.send({ ready: true });
  `
    const active = run(`
    import { spawnManaged, shutdown } from ${JSON.stringify(supervisor)};
    const child = spawnManaged(process.execPath, ['--input-type=module', '-e', ${JSON.stringify(child)}], {
      stdio: ['ignore', 'inherit', 'inherit', 'ipc'],
    });
    child.on('message', () => shutdown(130));
  `)
    try {
      const [code] = await once(active, 'close')
      assert.equal(code, 130)
      assert.equal(await readFile(marker, 'utf8'), 'cleaned')
    } finally {
      await rm(state, { recursive: true, force: true })
    }
  },
)

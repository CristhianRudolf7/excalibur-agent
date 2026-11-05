import { chromium, expect } from '@playwright/test'
import { spawn, execFile } from 'node:child_process'
import { promisify } from 'node:util'
import { access, mkdir, writeFile, rm } from 'node:fs/promises'
import { productionBrowser } from '../build-browser.ts'
import { startPreview, baseURL } from '../server.ts'
const exec = promisify(execFile)
const out = process.env.QA_OUT ?? 'test-results/shots/final'
await mkdir(out, { recursive: true })
const stop = await startPreview()
let display = 109
while (
  await access(`/tmp/.X11-unix/X${display}`).then(
    () => true,
    () => false,
  )
)
  display++
const env = { ...process.env, DISPLAY: `:${display}` }
const xvfb = spawn('Xvfb', [`:${display}`, '-screen', '0', '1920x1080x24', '-nolisten', 'tcp'], {
  stdio: 'ignore',
})
await writeFile(`${out}/qa-display.pid`, String(xvfb.pid))
let browser: Awaited<ReturnType<typeof chromium.launch>> | undefined
const results: unknown[] = []
try {
  await new Promise((resolve) => setTimeout(resolve, 500))
  browser = productionBrowser(
    await chromium.launch({
      channel: 'chrome',
      headless: false,
      ignoreDefaultArgs: ['--disable-back-forward-cache'],
      env,
      args: ['--window-size=1536,1000'],
    }),
  )
  const context = await browser.newContext({ viewport: null, reducedMotion: 'reduce' })
  const page = await context.newPage()
  await page.addInitScript(() =>
    addEventListener('pageshow', (event) =>
      sessionStorage.setItem('qa-bfcache', String(event.persisted)),
    ),
  )
  await page.goto(baseURL)
  await page.evaluate(() => document.fonts.ready)
  const windows = await exec('xdotool', ['search', '--onlyvisible', '--name', 'Excalibur'], { env })
  const windowId = windows.stdout.trim().split('\n').at(-1)!
  await exec('xdotool', ['windowfocus', '--sync', windowId], { env })
  const key = async (value: string) => {
    await exec('xdotool', ['key', '--clearmodifiers', value], { env })
    await page.waitForTimeout(250)
  }
  await key('ctrl+0')
  const initialRatio = await page.evaluate(() => devicePixelRatio)
  for (const [zoom, keys] of [
    [0.8, ['ctrl+minus', 'ctrl+minus']],
    [1.25, ['ctrl+plus', 'ctrl+plus']],
  ] as const) {
    await key('ctrl+0')
    for (const value of keys) await key(value)
    const measured = await page.evaluate(() => ({
      ratio: devicePixelRatio,
      width: innerWidth,
      height: innerHeight,
    }))
    expect(measured.ratio / initialRatio).toBeCloseTo(zoom, 2)
    for (const theme of ['light', 'dark'] as const) {
      await page.emulateMedia({ colorScheme: theme })
      await page.goto(baseURL)
      for (const section of ['top', 'machine', 'install', 'extensions']) {
        await page.locator(`#${section}`).scrollIntoViewIfNeeded()
        expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
          true,
        )
        await page.screenshot({ path: `${out}/zoom-${zoom * 100}-${theme}-${section}.png` })
        if (section === 'top')
          await exec(
            'ffmpeg',
            [
              '-loglevel',
              'error',
              '-y',
              '-f',
              'x11grab',
              '-video_size',
              '1920x1080',
              '-i',
              env.DISPLAY,
              '-frames:v',
              '1',
              `${out}/zoom-${zoom * 100}-${theme}-window.png`,
            ],
            { env },
          )
      }
      await page.goto(`${baseURL}/docs/getting-started/install/`)
      expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(
        true,
      )
      await page.screenshot({ path: `${out}/zoom-${zoom * 100}-${theme}-docs.png` })
      await exec(
        'ffmpeg',
        [
          '-loglevel',
          'error',
          '-y',
          '-f',
          'x11grab',
          '-video_size',
          '1920x1080',
          '-i',
          env.DISPLAY,
          '-frames:v',
          '1',
          `${out}/zoom-${zoom * 100}-${theme}-docs-window.png`,
        ],
        { env },
      )
    }
    results.push({ zoom, ...measured })
  }
  await key('ctrl+0')
  await page.goto(baseURL)
  const focused: string[] = []
  for (let i = 0; i < 40; i++) {
    await page.keyboard.press('Tab')
    await page.waitForTimeout(80)
    const entry = await page.evaluate(() => {
      const el = document.activeElement as HTMLElement
      if (el.tagName === 'BODY') return null
      return {
        name: el.textContent?.trim() || el.getAttribute('aria-label'),
        outline: getComputedStyle(el).outlineStyle,
      }
    })
    if (!entry) continue
    expect(entry.outline).not.toBe('none')
    focused.push(entry.name!)
  }
  results.push({ keyboard: focused })
  expect(focused.some((name) => name.includes('Full instructions'))).toBe(true)
  await page.emulateMedia({ reducedMotion: 'no-preference' })
  await page.goto(`${baseURL}/#install`)
  await expect(page.locator('#install')).toHaveAttribute('data-initialized', 'true')
  await page.waitForTimeout(700)
  await page.goto(`${baseURL}/docs/`)
  await page.goBack({ waitUntil: 'commit' })
  const historyPersisted = await page.evaluate(() => sessionStorage.getItem('qa-bfcache'))
  results.push({ historyPersisted })
  expect(historyPersisted).toBe('true')
  await expect(page.locator('#install')).toHaveAttribute('data-initialized', 'true')
  await page.locator('[data-install-tab]').last().click()
  await expect(page.locator('[data-install-tab]').last()).toHaveAttribute('aria-selected', 'true')
  await context.close()
} finally {
  await writeFile(`${out}/zoom-keyboard-history.json`, JSON.stringify(results, null, 2))
  await browser?.close()
  xvfb.kill('SIGTERM')
  await rm(`${out}/qa-display.pid`, { force: true })
  await stop()
}
console.log(JSON.stringify(results, null, 2))

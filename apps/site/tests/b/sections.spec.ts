import { expect, test } from '../browser'
import { baseURL } from '../server'
import { serveBuild } from './static'

test.beforeEach(async ({ page }) => {
  await serveBuild(page.context())
})

const sections = ['code', 'work', 'machine', 'measured', 'extensions']
for (const width of [360, 390, 768, 1024, 1280, 1366, 1536, 1920]) {
  for (const theme of ['light', 'dark'] as const)
    test(`B sections fit and remain readable: ${width} ${theme}`, async ({ page }) => {
      const errors: string[] = []
      page.on('pageerror', (error) => errors.push(error.message))
      await page.setViewportSize({ width, height: 900 })
      await page.emulateMedia({ reducedMotion: 'reduce', colorScheme: theme })
      await page.goto('/')
      await page.evaluate(() => document.fonts.ready)
      await expect(page.locator('.code-circuit')).toHaveCount(1)
      expect(
        await page.evaluate(() => document.documentElement.scrollWidth - innerWidth),
      ).toBeLessThanOrEqual(0)
      const defects = await page.evaluate(
        (names) =>
          names.flatMap((name) => {
            const section = document.querySelector<HTMLElement>(`#${name}`)!
            const box = section.getBoundingClientRect()
            return [
              ...section.querySelectorAll<HTMLElement>(
                'h2,h3,p,.code-node,.code-agent,.code-diff,.code-preview,.code-test,.machine-note,.extensions-module-note',
              ),
            ]
              .filter((el) => !el.classList.contains('sr-only'))
              .flatMap((el) => {
                const r = el.getBoundingClientRect()
                return r.right > box.right + 1 || r.left < box.left - 1 || r.bottom > box.bottom + 1
                  ? [`${name}: ${el.className} extends outside section`]
                  : []
              })
          }),
        sections,
      )
      expect(defects).toEqual([])
      expect(errors).toEqual([])
    })
}
test('benchmark ticks share one 0–100 linear axis', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/#measured')
  await page.evaluate(() => document.fonts.ready)
  const marks = await page.locator('.measured-rule').evaluateAll((rules) =>
    rules.map((rule) => {
      const r = rule.getBoundingClientRect(),
        tick = rule.querySelector<HTMLElement>('[data-score]')!,
        t = tick.getBoundingClientRect()
      return {
        start: r.left,
        end: r.right,
        score: Number(tick.dataset.score),
        position: ((t.left + t.width / 2 - r.left) / r.width) * 100,
      }
    }),
  )
  for (const mark of marks) {
    expect(mark.position).toBeCloseTo(mark.score, 1)
    expect(mark.start).toBeCloseTo(marks[0]!.start, 1)
    expect(mark.end).toBeCloseTo(marks[0]!.end, 1)
  }
  await expect(page.locator('.measured-axis')).toHaveText('0100')
  await expect(page.locator('.measured-number')).toHaveText('59.6%')
  const reservedGap = await page.locator('[data-measured-number]').evaluate((el) => {
    const range = document.createRange()
    range.selectNodeContents(el)
    return el.getBoundingClientRect().right - range.getBoundingClientRect().right
  })
  expect(Math.abs(reservedGap)).toBeLessThan(1)
})
test('machine distances come from the translated layers, pin releases, and final anchors land', async ({
  page,
}) => {
  await page.setViewportSize({ width: 1536, height: 864 })
  await page.goto('/')
  const machine = page.locator('#machine')
  await machine.scrollIntoViewIfNeeded()
  await expect(machine).toHaveAttribute('data-initialized', 'true')
  await page.waitForTimeout(450)
  const top = await page
    .locator('.machine-track')
    .evaluate((el) => el.getBoundingClientRect().top + scrollY)
  for (const offset of [150, 650, 1200]) {
    await page.evaluate((y) => scrollTo(0, y), top + offset)
    await page.waitForTimeout(650)
    expect(
      Math.abs(
        await page.locator('.machine-canvas').evaluate((el) => el.getBoundingClientRect().top),
      ),
    ).toBeLessThan(1)
    const values = await machine.evaluate((el) => {
      const scale = el.getBoundingClientRect().width / 1536
      const layers = [...el.querySelectorAll<HTMLElement>('[data-machine-layer]')]
      const positions = layers.map((layer) => layer.getBoundingClientRect().top)
      return layers.map((layer, i) => ({
        actual: (positions[i]! - positions[0]!) / scale / 10,
        label: parseFloat(layer.querySelector('[data-machine-z]')!.textContent!.split('=')[1]!),
        gap:
          i === layers.length - 1
            ? null
            : parseFloat(el.querySelectorAll('[data-machine-distance]')[i]!.textContent!),
        actualGap:
          i === layers.length - 1 ? null : (positions[i + 1]! - positions[i]!) / scale / 10,
      }))
    })
    for (const v of values) {
      expect(Math.abs(v.actual - v.label)).toBeLessThanOrEqual(0.06)
      if (v.gap !== null) expect(Math.abs(v.actualGap! - v.gap)).toBeLessThanOrEqual(0.06)
    }
  }
  await page.evaluate((y) => scrollTo(0, y), top + 2500)
  await page.waitForTimeout(650)
  expect(await machine.evaluate((el) => el.getBoundingClientRect().bottom)).toBeLessThan(864)
  for (const hash of ['measured', 'extensions']) {
    await page.evaluate((hash) => {
      location.hash = hash
    }, hash)
    await page.waitForTimeout(650)
    const y = await page.locator(`#${hash}`).evaluate((el) => el.getBoundingClientRect().top)
    expect(y).toBeGreaterThanOrEqual(-1)
    expect(y).toBeLessThanOrEqual(18)
  }
})
test('direct links to final sections survive lazy pin initialization', async ({ page }) => {
  for (const hash of ['measured', 'extensions']) {
    await page.goto(`/#${hash}`)
    await page.waitForTimeout(1500)
    const y = await page.locator(`#${hash}`).evaluate((el) => el.getBoundingClientRect().top)
    expect(y).toBeGreaterThanOrEqual(-1)
    expect(y).toBeLessThanOrEqual(18)
  }
})
test('no-JS and reduced motion preserve all five complete compositions', async ({ browser }) => {
  for (const noJS of [true, false])
    for (const theme of ['light', 'dark'] as const) {
      const page = await browser.newPage({
        javaScriptEnabled: !noJS,
        reducedMotion: 'reduce',
        colorScheme: theme,
        viewport: { width: 390, height: 844 },
      })
      await serveBuild(page.context())
      await page.goto(baseURL)
      for (const id of sections) {
        await page.locator(`#${id}`).scrollIntoViewIfNeeded()
        await expect(page.locator(`#${id} h2`).first()).toBeVisible()
      }
      await expect(page.locator('.code-preview')).toBeVisible()
      await expect(page.locator('.pin-spacer')).toHaveCount(0)
      await expect(page.locator('.extensions-cta')).toHaveAttribute(
        'href',
        'https://github.com/CristhianRudolf7/excalibur-agent',
      )
      expect(await page.locator('.code-preview a,.code-preview button').count()).toBe(0)
      await page.close()
    }
})
test('final rail ends at the sword and has no bottom exit', async ({ page }) => {
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.goto('/#extensions')
  const ends = await page.locator('#extensions .desktop-line [data-line-path]').evaluate((node) => {
    const path = node as SVGPathElement,
      p = path.getPointAtLength(path.getTotalLength()),
      end = new DOMPoint(p.x, p.y).matrixTransform(path.getScreenCTM()!)
    const sword = document.querySelector('.extensions-sword')!.getBoundingClientRect()
    return {
      x: end.x,
      y: end.y,
      left: sword.left,
      right: sword.right,
      top: sword.top,
      bottom: sword.bottom,
    }
  })
  expect(ends.x).toBeGreaterThanOrEqual(ends.left)
  expect(ends.x).toBeLessThanOrEqual(ends.right)
  expect(ends.y).toBeGreaterThan(ends.top)
  expect(ends.y).toBeLessThan(ends.bottom)
})

for (const width of [390, 1280, 1536, 1920])
  test(`B rail joins are exact at ${width}`, async ({ page }) => {
    await page.setViewportSize({ width, height: 864 })
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await page.goto('/')
    const gaps = await page.evaluate(() => {
      const selector = innerWidth < 1024 ? '.mobile-line' : '.desktop-line'
      const paths = ['code', 'work', 'machine', 'measured', 'install', 'extensions'].map((name) =>
        document.querySelector<SVGPathElement>(`#${name} ${selector} [data-line-path]`)!,
      )
      return paths.slice(1).map((path, i) => {
        const previous = paths[i]!,
          a = previous.getPointAtLength(previous.getTotalLength()),
          b = path.getPointAtLength(0)
        const p = new DOMPoint(a.x, a.y).matrixTransform(previous.getScreenCTM()!),
          q = new DOMPoint(b.x, b.y).matrixTransform(path.getScreenCTM()!)
        return Math.hypot(p.x - q.x, p.y - q.y)
      })
    })
    for (const gap of gaps) expect(gap).toBeLessThanOrEqual(1)
  })

test('B motion cleans up on resize and reduced motion restores exploded blueprint', async ({
  page,
}) => {
  const errors: string[] = []
  page.on('pageerror', (error) => errors.push(error.message))
  await page.setViewportSize({ width: 1536, height: 864 })
  await page.goto('/#machine')
  await page.waitForSelector('#machine[data-initialized]')
  await page.waitForTimeout(400)
  await expect(page.locator('.machine-canvas')).toHaveCSS('position', 'sticky')
  await page.setViewportSize({ width: 390, height: 844 })
  await page.waitForTimeout(500)
  await expect(page.locator('.machine-canvas')).not.toHaveCSS('position', 'sticky')
  await page.setViewportSize({ width: 1280, height: 864 })
  await page.waitForTimeout(500)
  await expect(page.locator('.machine-canvas')).toHaveCSS('position', 'sticky')
  await page.emulateMedia({ reducedMotion: 'reduce' })
  await page.waitForTimeout(400)
  await expect(page.locator('.pin-spacer')).toHaveCount(0)
  const transforms = await page
    .locator('[data-machine-layer]')
    .evaluateAll((nodes) => nodes.map((node) => getComputedStyle(node).transform))
  expect(transforms.every((value) => value === 'none')).toBe(true)
  await expect(page.locator('[data-machine-z]').nth(1)).toHaveText('z = 12.0 mm')
  expect(errors).toEqual([])
})

test('cached circular paths match native Chrome geometry within one pixel', async ({ page }) => {
  await page.setViewportSize({ width: 1536, height: 864 })
  await page.goto('/')
  for (const name of sections) {
    await page.locator(`#${name}`).evaluate((el) => el.scrollIntoView({ block: 'start' }))
    await page.waitForSelector(`#${name}[data-initialized]`)
  }
  const errors = await page.evaluate(
    (names) =>
      names.flatMap((name) => {
        const paths = [...document.querySelectorAll<SVGPathElement>(`#${name} [data-line-path]`)]
        return paths.map((path) => {
          const nativeLength = SVGGeometryElement.prototype.getTotalLength.call(path)
          const length = path.getTotalLength()
          let max = 0
          for (let i = 0; i <= 720; i++) {
            const actual = SVGGeometryElement.prototype.getPointAtLength.call(
              path,
              (nativeLength * i) / 720,
            )
            const cached = path.getPointAtLength((length * i) / 720)
            max = Math.max(max, Math.hypot(actual.x - cached.x, actual.y - cached.y))
          }
          return { name, max, lengthError: Math.abs(nativeLength - length) }
        })
      }),
    sections,
  )
  for (const result of errors) {
    expect(result.max, `${result.name} point error`).toBeLessThan(1)
    expect(result.lengthError, `${result.name} length error`).toBeLessThan(1)
  }
})

test('machine holds its centered canvas for exactly 150vh at every desktop width', async ({
  page,
}) => {
  for (const width of [1024, 1280, 1536, 1920]) {
    await page.setViewportSize({ width, height: 1080 })
    await page.goto('/#machine')
    await page.waitForSelector('#machine[data-initialized]')
    const top = await page
      .locator('#machine')
      .evaluate((el) => el.getBoundingClientRect().top + scrollY)
    await page.evaluate((y) => scrollTo(0, y), top + 700)
    await page.waitForTimeout(500)
    const geometry = await page.locator('#machine').evaluate((el) => {
      const stage = el.getBoundingClientRect(),
        canvas = el.querySelector('.machine-canvas')!.getBoundingClientRect()
      return {
        left: canvas.left,
        top: canvas.top,
        travel: stage.height - canvas.height,
        expectedTravel: innerHeight * 1.5,
        expectedLeft: (innerWidth - Math.min(innerWidth, 1536)) / 2,
      }
    })
    expect(Math.abs(geometry.left - geometry.expectedLeft)).toBeLessThan(1)
    expect(Math.abs(geometry.top)).toBeLessThan(1)
    expect(Math.abs(geometry.travel - geometry.expectedTravel)).toBeLessThan(1)
    await page.evaluate((y) => scrollTo(0, y), top + 1080 * 1.5 + 100)
    await page.waitForTimeout(500)
    expect(
      await page.locator('.machine-canvas').evaluate((el) => el.getBoundingClientRect().top),
    ).toBeLessThan(-90)
  }
})

test('the closing pen, preview and modules stop at the sword', async ({ page }) => {
  await page.goto('/#extensions')
  await page.waitForSelector('#extensions[data-initialized]')
  await page.evaluate(() => scrollTo(0, document.documentElement.scrollHeight - innerHeight))
  await page.waitForTimeout(1000)
  const snapshot = () =>
    page.locator('#extensions').evaluate((el) => ({
      modules: [...el.querySelectorAll<HTMLElement>('[data-extension-module]')].map(
        (node) => node.style.transform,
      ),
      dots: [...el.querySelectorAll('.desktop-line [data-preview],.desktop-line [data-pen]')].map(
        (node) => getComputedStyle(node).opacity,
      ),
      offsets: [...el.querySelectorAll('[data-extension-z]')].map((node) => node.textContent),
    }))
  const first = await snapshot()
  expect(first.dots.every((opacity) => opacity === '0')).toBe(true)
  expect(first.offsets.every((value) => value === 'z = 0.0 mm')).toBe(true)
  await page.waitForTimeout(600)
  expect(await snapshot()).toEqual(first)
})

for (const width of [1280, 1536, 1920]) {
  test(`sticky machine rail spans its reserved journey and keeps a round pen at ${width}`, async ({
    page,
  }) => {
    await page.setViewportSize({ width, height: (width * 9) / 16 })
    await page.goto('/#machine')
    await page.waitForSelector('#machine[data-initialized]')
    await page
      .locator('#machine')
      .evaluate((el) => scrollTo(0, el.getBoundingClientRect().top + scrollY + innerHeight * 0.75))
    await page.waitForTimeout(450)
    const result = await page.locator('#machine').evaluate((section) => {
      const path = section.querySelector<SVGPathElement>('.desktop-line [data-line-path]')!
      const p = path.getPointAtLength(path.getTotalLength())
      const end = new DOMPoint(p.x, p.y).matrixTransform(path.getScreenCTM()!)
      const pen = section.querySelector('.desktop-line [data-pen]')!.getBoundingClientRect()
      return {
        gap: Math.abs(end.y - section.getBoundingClientRect().bottom),
        oval: Math.abs(pen.width - pen.height),
        y: pen.top,
      }
    })
    expect(result.gap).toBeLessThanOrEqual(1)
    expect(result.oval).toBeLessThanOrEqual(0.1)
    expect(result.y).toBeGreaterThan(0)
    expect(result.y).toBeLessThan((width * 9) / 16)
  })
}

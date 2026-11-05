import { mkdir } from 'node:fs/promises'
import { test, expect, type Page } from './fixtures'
import { mockChat } from './chat-fixture'
import { mockEvents } from './event-fixture'

// Recorded daemon-shaped events drive the real WebSocket client and chat.
// No daemon credentials or external model calls are used by this fixture.
for (const width of [1440, 390]) {
  test(`live coding activity, nested runs and stable layout at ${width}px`, async ({ page }) => {
    await page.setViewportSize({ width, height: 900 })
    await page.addInitScript(() => {
      localStorage.setItem('excalibur-ui-theme', 'light')
      localStorage.setItem('excalibur-ui-language', 'en')
    })
    await mockChat(page, [
      { role: 'user', text: 'Add a health endpoint and verify it with tests.' },
    ])
    const bus = await mockEvents(page)
    await page.goto('/agents/excalibur?view=chat')
    await bus.ready('file.edited')
    const parent = { conversation_id: 'cnv_web_excalibur', run_id: 'parent', turn_id: 'parent' }
    const child = { ...parent, run_id: 'child', parent_run_id: 'parent' }
    bus.emit('run.progress', { ...parent, state: 'running' })
    bus.emit('run.progress', {
      ...child,
      name: 'Implement health endpoint',
      state: 'running',
      steps: ['Inspect the router', 'Add the health endpoint', 'Verify request handling'],
      plan_done: 0,
      plan_total: 3,
    })
    bus.emit('tool.called', {
      ...child,
      call_id: 'read',
      tool: 'read_file',
      kind: 'read',
      path: 'internal/server/routes.go',
      status: 'running',
    })
    const feed = page.getByRole('region', { name: 'Coding activity' })
    await expect(feed).toBeVisible()
    await expect(feed.getByText('Implement health endpoint')).toBeVisible()
    await expect(feed.locator('[data-run="child"]').first()).toHaveAttribute(
      'data-parent-run',
      'parent',
    )
    bus.emit('tool.completed', {
      ...child,
      call_id: 'read',
      tool: 'read_file',
      kind: 'read',
      path: 'internal/server/routes.go',
      status: 'completed',
      elapsed_ms: 24,
    })
    bus.emit('tool.called', {
      ...child,
      call_id: 'edit',
      tool: 'edit_file',
      kind: 'edit',
      path: 'internal/server/routes.go',
      status: 'running',
    })
    bus.emit('tool.completed', {
      ...child,
      call_id: 'edit',
      tool: 'edit_file',
      kind: 'edit',
      path: 'internal/server/routes.go',
      status: 'completed',
    })
    bus.emit('file.edited', {
      ...child,
      call_id: 'edit',
      edit_id: 'version-1',
      path: 'internal/server/routes.go',
      added: 3,
      removed: 1,
      diff: '@@ -8,1 +8,3 @@\n-router.Handle("/", index)\n+router.Handle("/", index)\n+router.Handle("/health", health)\n+// Health requests are handled locally.\n',
    })
    const edit = feed.getByRole('button', { name: 'Edit: internal/server/routes.go' }).last()
    await expect(edit).toHaveAttribute('aria-expanded', 'false')
    await edit.click()
    await expect(feed.getByText('+router.Handle("/health", health)')).toBeVisible()
    await edit.click()
    bus.emit('tool.called', {
      ...child,
      call_id: 'test',
      tool: 'exec',
      kind: 'command',
      command: 'go test -v ./internal/server',
      status: 'running',
    })
    const command = feed.getByRole('button', { name: 'Tests: go test -v ./internal/server' })
    await expect(command).toBeVisible()
    const before = await feed.boundingBox()
    const composerBefore = await page.locator('.composer-area').boundingBox()
    bus.emit('tool.completed', {
      ...child,
      call_id: 'test',
      tool: 'exec',
      kind: 'command',
      command: 'go test -v ./internal/server',
      status: 'completed',
      exit_code: 0,
      elapsed_ms: 843,
      output:
        '=== RUN   TestHealth\n--- PASS: TestHealth (0.00s)\n=== RUN   TestNotFound\n--- PASS: TestNotFound (0.00s)\nPASS\nok  example/internal/server 0.843s',
    })
    await expect(feed.getByText('2 passed')).toBeVisible()
    await expect(feed.getByText('Exit 0')).toBeVisible()
    expect(await feed.boundingBox()).toMatchObject({ height: before!.height })
    expect((await page.locator('.composer-area').boundingBox())!.y).toBe(composerBefore!.y)
    await expect(command).toHaveCount(1)
    await command.click()
    await expect(feed.locator('.coding-details')).toContainText('TestHealth')
    await command.click()
    bus.emit('run.progress', {
      ...child,
      state: 'completed',
      elapsed_ms: 7240,
      plan_done: 3,
      plan_total: 3,
    })
    bus.emit('run.progress', { ...parent, state: 'completed', elapsed_ms: 7500 })
    bus.emit('message.created', {
      ...parent,
      message_id: 'final',
      role: 'assistant',
      text: 'Added the health endpoint. Both request tests pass.',
      changes: [
        { path: 'internal/server/routes.go', status: 'modified', added: 3, removed: 1, hunks: [] },
      ],
    })
    const summary = feed.getByRole('button', {
      name: /Coding activity · 3 actions · 1 subagent · Activity finished/,
    })
    await expect(summary).toHaveAttribute('aria-expanded', 'false')
    await expect(feed.getByRole('log')).toHaveCount(0)
    await expect
      .poll(() =>
        page.locator('.message-history').evaluate((node) => {
          const user = node.querySelector('.message-user')!
          const activity = node.querySelector('.coding-activity')!
          const final = [...node.querySelectorAll('.message-agent')].find((row) =>
            row.textContent?.includes('Added the health endpoint.'),
          )!
          return (
            !!(user.compareDocumentPosition(activity) & Node.DOCUMENT_POSITION_FOLLOWING) &&
            !!(activity.compareDocumentPosition(final) & Node.DOCUMENT_POSITION_FOLLOWING)
          )
        }),
      )
      .toBe(true)
    await summary.click()
    await expect(summary).toHaveAttribute('aria-expanded', 'true')
    await expect(page.locator('.changes-line')).toBeVisible()
    await feed.getByRole('button', { name: 'Steps: 3 of 3' }).click()
    await expect(feed.getByText('Verify request handling')).toBeVisible()
    // Scroll intent remains under the reader's control as newer events arrive.
    await feed.locator('.coding-scroll').evaluate((node) => {
      node.scrollTop = 0
      node.dispatchEvent(new Event('scroll'))
    })
    await feed.getByRole('button', { name: 'Steps: 3 of 3' }).click()
    await summary.click()
    await expect(feed).toHaveJSProperty('offsetHeight', 44)
    const shots = 'test-results/coding-view'
    await mkdir(shots, { recursive: true })
    for (const dark of [false, true]) {
      await page.evaluate((value) => document.documentElement.classList.toggle('dark', value), dark)
      await page.screenshot({
        path: `${shots}/${width === 390 ? 'mobile-390' : 'desktop'}-${dark ? 'dark' : 'light'}.png`,
        animations: 'disabled',
      })
    }
    expect(await page.evaluate(() => document.documentElement.scrollWidth <= innerWidth)).toBe(true)
    await summary.click()
    await page.emulateMedia({ reducedMotion: 'reduce' })
    await expect(feed.locator('.coding-entry-toggle').first()).toHaveCSS(
      'transition-duration',
      '0s',
    )
    // An unrelated conversation cannot inject foreground activity.
    bus.emit('tool.called', {
      ...child,
      conversation_id: 'other',
      call_id: 'foreign',
      tool: 'exec',
      kind: 'command',
      command: 'foreign-command',
      status: 'running',
    })
    await expect(feed.getByText('foreign-command')).toHaveCount(0)
  })
}

async function atEnd(page: Page) {
  await expect
    .poll(() =>
      page
        .locator('.coding-scroll')
        .evaluate((node) => Math.abs(node.scrollHeight - node.scrollTop - node.clientHeight)),
    )
    .toBeLessThan(3)
}

for (const width of [1440, 390]) {
  test(`long delegated build follows live events and preserves scroll intent at ${width}px`, async ({
    page,
  }) => {
    test.setTimeout(120_000)
    await page.setViewportSize({ width, height: 900 })
    await page.addInitScript(() => {
      localStorage.setItem('excalibur-ui-theme', 'light')
      localStorage.setItem('excalibur-ui-language', 'en')
    })
    await mockChat(page, [
      { role: 'user', text: 'Build the service, delegate implementation and verify every module.' },
    ])
    const bus = await mockEvents(page)
    await page.goto('/agents/excalibur?view=chat')
    await bus.ready('file.edited')
    const root = { conversation_id: 'cnv_web_excalibur', run_id: 'build', turn_id: 'build' }
    const worker = { ...root, run_id: 'worker', parent_run_id: 'build' }
    const nested = { ...root, run_id: 'nested', parent_run_id: 'worker' }
    const verifier = { ...root, run_id: 'verifier', parent_run_id: 'nested' }
    for (const [run, name] of [
      [root, 'Build service'],
      [worker, 'Implement modules'],
      [nested, 'Verify modules'],
    ] as const)
      bus.emit('run.progress', {
        ...run,
        state: 'running',
        name,
        steps: ['Inspect', 'Implement', 'Verify'],
      })
    const feed = page.getByRole('region', { name: 'Coding activity' })
    await expect(feed).toBeInViewport({ ratio: 1 })
    const initialHeight = (await feed.boundingBox())!.height
    const composerY = (await page.locator('.composer-area').boundingBox())!.y
    const scroll = feed.getByRole('log')
    const append = (index: number, run = root) => {
      const command = `go test -v ./internal/module${index}`
      bus.emit('tool.called', {
        ...run,
        call_id: `command-${index}`,
        tool: 'exec',
        kind: 'command',
        command,
        status: 'running',
      })
      bus.emit('tool.completed', {
        ...run,
        call_id: `command-${index}`,
        tool: 'exec',
        kind: 'command',
        command,
        status: 'completed',
        exit_code: 0,
        output: `--- PASS: TestModule${index} (0.00s)\nPASS\nok example/internal/module${index} 0.043s`,
      })
    }
    // More than 600 events, including retention eviction and nested concurrent work.
    for (let batch = 0; batch < 16; batch++) {
      for (let index = batch * 20; index < (batch + 1) * 20; index++)
        append(index, index < 260 ? root : index % 2 ? nested : worker)
      await expect(
        feed.getByRole('button', {
          name: `Tests: go test -v ./internal/module${(batch + 1) * 20 - 1}`,
        }),
      ).toBeInViewport()
      await atEnd(page)
      expect((await feed.boundingBox())!.height).toBe(initialHeight)
    }
    // Repeated assistant notices must not drag the outer viewport below the feed.
    for (let index = 0; index < 12; index++)
      bus.emit('message.created', {
        ...root,
        message_id: `notice-${index}`,
        role: 'assistant',
        text: `I started a subagent to verify module ${index}.`,
      })
    bus.emit('run.progress', {
      ...verifier,
      name: 'Final integration checks',
      state: 'running',
      steps: ['Run integration suite', 'Review results'],
    })
    for (let index = 320; index < 323; index++) append(index, verifier)
    await expect(
      feed.getByRole('button', { name: 'Tests: go test -v ./internal/module322' }),
    ).toBeInViewport()
    await expect(feed).toBeInViewport({ ratio: 1 })
    await atEnd(page)
    await expect(feed.locator('[data-run="verifier"]').first()).toHaveAttribute(
      'data-parent-run',
      'nested',
    )
    expect(await feed.locator('.coding-virtual-row').count()).toBeLessThan(40)
    expect((await page.locator('.composer-area').boundingBox())!.y).toBe(composerY)
    expect(await page.evaluate(() => document.documentElement.scrollHeight <= innerHeight)).toBe(
      true,
    )
    await expect(feed.getByText('323 actions · 3 subagents')).toBeVisible()
    if (width === 1440) {
      const shots = 'test-results/coding-view'
      await mkdir(shots, { recursive: true })
      await page.screenshot({ path: `${shots}/long-build.png`, animations: 'disabled' })
    }
    // Scroll up to read earlier work; appends and measured height changes must not yank it back.
    await scroll.evaluate((node) => {
      node.scrollTop -= 600
      node.dispatchEvent(new Event('scroll'))
    })
    const pausedTop = await scroll.evaluate((node) => node.scrollTop)
    for (let index = 323; index < 333; index++) append(index, verifier)
    await expect(feed.getByText('333 actions · 3 subagents')).toBeVisible()
    await expect.poll(() => scroll.evaluate((node) => node.scrollTop)).toBe(pausedTop)
    await scroll.evaluate((node) => {
      node.scrollTop = node.scrollHeight
      node.dispatchEvent(new Event('scroll'))
    })
    append(333, verifier)
    await expect(
      feed.getByRole('button', { name: 'Tests: go test -v ./internal/module333' }),
    ).toBeInViewport()
    await atEnd(page)
    for (const run of [verifier, nested, worker, root])
      bus.emit('run.progress', { ...run, state: 'completed', elapsed_ms: 600_000 })
    bus.emit('message.created', {
      ...root,
      message_id: 'build-final',
      role: 'assistant',
      text: 'The build is complete and all module checks pass.',
    })
    const summary = feed.getByRole('button', {
      name: /334 actions · 3 subagents · Activity finished/,
    })
    await expect(summary).toHaveAttribute('aria-expanded', 'false')
    await expect(feed.getByRole('log')).toHaveCount(0)
    await expect(
      page.getByText('The build is complete and all module checks pass.'),
    ).toBeInViewport()
    // Virtualization may unmount older messages; each collapsed summary still precedes its final reply.
    expect(
      await feed.evaluate(
        (node) =>
          !!(
            node.compareDocumentPosition(
              [...document.querySelectorAll('.message-agent')].find((row) =>
                row.textContent?.includes('The build is complete'),
              )!,
            ) & Node.DOCUMENT_POSITION_FOLLOWING
          ),
      ),
    ).toBe(true)
    await summary.click()
    await expect(feed.getByText('60 earlier actions omitted')).toBeVisible()
    await expect(scroll).toHaveCSS('height', '320px')
  })
}

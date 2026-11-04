import { test, expect } from './fixtures'
import { labSession, mockRobotLab } from './robotlab-fixture'

for (const status of ['live', 'stopped'] as const) {
  test(`opens an idle ${status} session using the previous attempt pose without live events`, async ({
    page,
  }) => {
    await page.setViewportSize({ width: 1440, height: 900 })
    const { sessions } = await mockRobotLab(page)
    sessions.set('idle-training', labSession('idle-training', status))
    const requested: { attempt: number; from: number; count: number }[] = []
    let release: () => void = () => {}
    const recording = new Promise<void>((resolve) => {
      release = resolve
    })
    await page.route('**/rpc', async (route) => {
      const request = route.request().postDataJSON()
      if (request.method !== 'robotlab.attempt.frames') return route.fallback()
      const { attempt, from, count } = request.params
      requested.push({ attempt, from, count })
      await recording
      const frames =
        attempt === 2
          ? []
          : [0, 1].map((seq) => ({
              session: 'idle-training',
              attempt: 1,
              seq,
              t: seq,
              poses: [0, 0, 0, 1, 0, 0, 0, 1 + seq, 0, 0.27, 1, 0, 0, 0],
              camera: { name: 'robot/head', pose: [seq, -2, 2, 0.9239, 0.3827, 0, 0] },
              held: null,
              status: 'stopped',
            }))
      await route.fulfill({
        json: {
          jsonrpc: '2.0',
          id: request.id,
          result: { frames: frames.slice(from, from + count), total: frames.length },
        },
      })
    })
    await page.goto('/lab?tab=trainings&session=idle-training')
    await expect(page.getByText('Waiting for the recorded pose…')).toHaveCount(2)
    release()
    await expect(page.getByText('Waiting for the recorded pose…')).toHaveCount(0)
    await expect(page.getByText('Waiting for the robot camera…')).toHaveCount(0)
    await expect(page.locator('.lab-observer canvas')).toBeVisible()
    await expect(page.locator('.lab-camera canvas')).toBeVisible()
    expect(requested).toContainEqual({ attempt: 1, from: 1, count: 1 })
    await page.screenshot({ path: test.info().outputPath(`${status}-initial-pose.png`) })
  })
}

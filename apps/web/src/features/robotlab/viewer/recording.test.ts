import { expect, it, vi } from 'vitest'
const call = vi.hoisted(() => vi.fn())
vi.mock('./rpc', () => ({ viewerCall: call }))
import { advanceTime, clampTime, fetchFrames, frameAt } from './recording'
import { frameFixture } from './fixtures.test-support'

it('pages through total using wire offsets and merges repeated sequences in sim-time order', async () => {
  call.mockReset()
  const frames = Array.from({ length: 805 }, (_, i) => frameFixture(i))
  frames[399] = frameFixture(398, { status: 'latest' })
  call.mockImplementation((_method, { from, count }) =>
    Promise.resolve({ frames: frames.slice(from, from + count), total: frames.length }),
  )
  const merged = await fetchFrames('session', 1, new AbortController().signal)
  expect(call.mock.calls.map(([, params]) => params)).toEqual([
    { session: 'session', attempt: 1, from: 0, count: 400 },
    { session: 'session', attempt: 1, from: 400, count: 400 },
    { session: 'session', attempt: 1, from: 800, count: 400 },
  ])
  expect(merged).toHaveLength(804)
  expect(merged[398]?.status).toBe('latest')
  expect(merged.at(-1)?.seq).toBe(804)
})
it('filters recordings by session/attempt and sorts by time instead of arrival', async () => {
  call.mockResolvedValueOnce({
    frames: [
      frameFixture(3),
      frameFixture(1),
      frameFixture(2, { session: 'other' }),
      frameFixture(4, { attempt: 2 }),
    ],
    total: 4,
  })
  expect(
    (await fetchFrames('session', 1, new AbortController().signal)).map((frame) => frame.seq),
  ).toEqual([1, 3])
})
it('terminates empty recordings, rejects incomplete pages, and honors cancellation', async () => {
  call.mockResolvedValueOnce({ frames: [], total: 0 })
  expect(await fetchFrames('session', 1, new AbortController().signal)).toEqual([])
  call.mockResolvedValueOnce({ frames: [], total: 10 })
  await expect(fetchFrames('session', 1, new AbortController().signal)).rejects.toThrow(
    'Incomplete',
  )
  const controller = new AbortController()
  controller.abort()
  await expect(fetchFrames('session', 1, controller.signal)).rejects.toThrow()
})
it('samples the 20 Hz timeline at speed, clamps seeking, and holds the last frame at the end', () => {
  const frames = Array.from({ length: 21 }, (_, i) => frameFixture(i))
  expect(frameAt([], 0)).toBeNull()
  expect(frameAt(frames, -1)?.seq).toBe(0)
  expect(frameAt(frames, 0.049)?.seq).toBe(0)
  expect(frameAt(frames, 0.05)?.seq).toBe(1)
  expect(frameAt(frames, advanceTime(0, 100, 2, 1))?.seq).toBe(4)
  expect(frameAt(frames, advanceTime(0.9, 100, 4, 1))?.seq).toBe(20)
  expect(clampTime(-1, 1)).toBe(0)
  expect(clampTime(NaN, 1)).toBe(0)
  expect(advanceTime(0.3, -100, 1, 1)).toBe(0.3)
})

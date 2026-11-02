import { expect, it, vi } from 'vitest'
import { applyOps, diffScene, scheduleElements, VersionBuffer } from './scene'
import type { Board, Element } from './types'
const element: Element = { id: 'a', type: 'rect', x: 1, y: 2, author: 'agent' }
const initial: Board = {
  id: 'board',
  conversation_id: 'conversation',
  title: 'Board',
  version: 0,
  elements: [],
  updated_at: '',
}
it('applies partial updates, deletion, clear and replacement without losing provenance', () => {
  const scene = applyOps([element], [{ op: 'update', elements: [{ id: 'a', x: 42 }] }])
  expect(scene[0]).toEqual({ ...element, x: 42 })
  expect(applyOps(scene, [{ op: 'remove', ids: ['a'] }])).toEqual([])
  expect(applyOps(scene, [{ op: 'clear' }])).toEqual([])
  expect(applyOps([], [{ op: 'set_scene', elements: [element] }])).toEqual([element])
  expect(applyOps([element], diffScene([element], scene))).toEqual(scene)
})
it('buffers out of order versions and ignores duplicates', async () => {
  let board = initial
  const fetch = vi.fn(async () => board)
  const render = vi.fn(async () => {})
  const buffer = new VersionBuffer(
    () => board,
    (value) => {
      board = value
    },
    fetch,
    render,
  )
  const second = buffer.receive({
    conversation_id: 'conversation',
    version: 2,
    ops: [{ op: 'update', elements: [{ id: 'a', x: 30 }] }],
  })
  const first = buffer.receive({
    conversation_id: 'conversation',
    version: 1,
    ops: [{ op: 'add', elements: [element] }],
  })
  await Promise.all([first, second])
  expect(board.version).toBe(2)
  expect(board.elements[0]?.x).toBe(30)
  await buffer.receive({ conversation_id: 'conversation', version: 1 })
  expect(render).toHaveBeenCalledTimes(2)
  expect(fetch).not.toHaveBeenCalled()
})
it('fetches the authoritative scene on a version gap and drains later versions', async () => {
  let board = initial
  const fetch = vi.fn(async () => ({ ...initial, version: 2, elements: [element] }))
  const buffer = new VersionBuffer(
    () => board,
    (value) => {
      board = value
    },
    fetch,
    async () => {},
  )
  await buffer.receive({
    conversation_id: 'conversation',
    version: 3,
    ops: [{ op: 'update', elements: [{ id: 'a', x: 90 }] }],
  })
  expect(fetch).toHaveBeenCalledTimes(1)
  expect(board.version).toBe(3)
  expect(board.elements[0]?.x).toBe(90)
})
it('schedules incremental elements and cancels pending animation', async () => {
  vi.useFakeTimers()
  const controller = new AbortController(),
    render = vi.fn()
  const work = scheduleElements(
    [{ op: 'add', elements: [element, { ...element, id: 'b' }] }],
    100,
    false,
    render,
    controller.signal,
  )
  expect(render).toHaveBeenCalledTimes(1)
  await vi.advanceTimersByTimeAsync(100)
  expect(render).toHaveBeenCalledTimes(2)
  controller.abort()
  await work
  vi.useRealTimers()
})
it('reduced motion applies a whole operation immediately', async () => {
  const render = vi.fn()
  await scheduleElements(
    [{ op: 'add', elements: [element, { ...element, id: 'b' }] }],
    100,
    true,
    render,
    new AbortController().signal,
  )
  expect(render).toHaveBeenCalledTimes(1)
  expect(render.mock.calls[0]![0][0].elements).toHaveLength(2)
})

it('reveals text, strokes and shapes progressively with the exact final element', async () => {
  const { revealFrames } = await import('./scene')
  for (const item of [
    element,
    { ...element, type: 'text' as const, text: 'A longer explanation' },
    {
      ...element,
      type: 'line' as const,
      points: [
        [0, 0],
        [100, 100],
      ] as const,
    },
    {
      ...element,
      type: 'freedraw' as const,
      points: [
        [0, 0],
        [1, 1],
        [2, 2],
        [3, 3],
      ] as const,
    },
  ]) {
    const frames = revealFrames(item)
    expect(frames).toHaveLength(6)
    expect(frames[5]).toEqual(item)
    expect(frames[0]).not.toEqual(item)
  }
})

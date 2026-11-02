import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
vi.mock('./PixiRenderer', () => ({ PixiRenderer: class {} }))
import { WorldEngine } from './WorldEngine'
import { TickerController } from './ticker'
import { activityMap } from '../activity-map'
import type { WorldRenderer } from './types'
import type { WorldAgent, WorldEvent } from '../events'
import type { WorldLayout, Room } from '../layout'
const room: Room = {
  id: 'first',
  label: 'First',
  type: 'project',
  polygon: [
    { x: 0, y: 0 },
    { x: 200, y: 0 },
    { x: 200, y: 200 },
    { x: 0, y: 200 },
  ],
  door: { x: 100, y: 190 },
  spots: { desk: { x: 100, y: 100 } },
  interiorSpots: { desk: { x: 500, y: 500 } },
  waypoints: [],
  deliverables: [],
}
const layout: WorldLayout = {
  id: 'arbitrary',
  width: 800,
  height: 600,
  rooms: [room],
  plaza: { x: 400, y: 400 },
}
const agent: WorldAgent = {
  id: 'custom-agent',
  name: 'Custom',
  homeRoomId: 'first',
  sprite: { variant: 'cap', tone: 'var(--pink)' },
}
function renderer(): WorldRenderer {
  return {
    mount: vi.fn(async () => {}),
    unmount: vi.fn(),
    setLayout: vi.fn(async () => {}),
    upsertAgent: vi.fn(),
    removeAgent: vi.fn(),
    moveAgent: vi.fn(),
    setCamera: vi.fn(),
    highlight: vi.fn(),
    render: vi.fn(),
    start: vi.fn(),
    stop: vi.fn(),
    resize: vi.fn(),
  }
}
beforeEach(() => {
  vi.useFakeTimers()
  vi.stubGlobal('matchMedia', () => ({
    matches: true,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      disconnect() {}
    },
  )
  vi.stubGlobal(
    'IntersectionObserver',
    class {
      observe() {}
      disconnect() {}
    },
  )
})
afterEach(() => {
  vi.useRealTimers()
  vi.unstubAllGlobals()
})
describe('standalone, renderer-independent world engine API', () => {
  it('renders and hit-tests a new room from config without changing engine code', async () => {
    const drawing = renderer(),
      engine = new WorldEngine({ layout, renderer: drawing }),
      host = document.createElement('div')
    await engine.mount(host)
    const added: Room = {
      ...room,
      id: 'new-room',
      polygon: room.polygon.map((point) => ({ x: point.x + 300, y: point.y })),
      door: { x: 400, y: 190 },
      spots: { desk: { x: 400, y: 100 } },
    }
    await engine.setLayout({ ...layout, rooms: [room, added] })
    expect(drawing.setLayout).toHaveBeenLastCalledWith(
      expect.objectContaining({ rooms: [room, added] }),
      undefined,
    )
    expect(engine.hitTest({ x: 400, y: 100 })?.id).toBe('new-room')
    const click = vi.fn(),
      remove = engine.onRoomClick(click)
    host.dispatchEvent(new MouseEvent('pointerdown', { clientX: 300, clientY: 75 }))
    host.dispatchEvent(new MouseEvent('pointerup', { clientX: 300, clientY: 75 }))
    vi.advanceTimersByTime(250)
    expect(click).toHaveBeenCalledWith('new-room')
    remove()
    engine.unmount()
    expect(drawing.unmount).toHaveBeenCalledOnce()
  })
  it('renders runtime helpers, updates arbitrary palettes and releases them on removal', async () => {
    const drawing = renderer(),
      engine = new WorldEngine({ layout, renderer: drawing })
    await engine.mount(document.createElement('div'))
    engine.upsertAgent(agent)
    const helper = { ...agent, id: 'runtime-helper', parentId: agent.id }
    engine.upsertAgent(helper)
    expect(drawing.upsertAgent).toHaveBeenLastCalledWith(expect.objectContaining({ agent: helper }))
    engine.upsertAgent({ ...helper, sprite: { variant: 'bow', tone: 'var(--green)' } })
    expect(engine.agents.get(helper.id)?.agent.sprite.variant).toBe('bow')
    engine.removeAgent(helper.id)
    expect(engine.agents.has(helper.id)).toBe(false)
    expect(drawing.removeAgent).toHaveBeenCalledWith(helper.id)
    engine.unmount()
  })
  it('applies a table-only activity extension and its spawn/remove payload', async () => {
    const drawing = renderer(),
      activities = {
        ...activityMap,
        audit: {
          ...activityMap.read!,
          roomType: 'project',
          spot: 'desk',
          animation: 'write' as const,
        },
      }
    const engine = new WorldEngine({ layout, renderer: drawing, activities })
    await engine.mount(document.createElement('div'))
    const event: WorldEvent = {
      id: 'audit',
      timestamp: 1,
      agentId: agent.id,
      activity: 'audit',
      status: 'progress',
      target: { projectId: 'a' },
      text: 'Auditing',
      literal: true,
      agentChanges: { upsert: [agent] },
    }
    engine.applyEvent(event)
    expect(engine.agents.get(agent.id)).toMatchObject({
      position: { x: 100, y: 100 },
      animation: 'write',
      roomId: 'first',
    })
    engine.applyEvent({ ...event, id: 'remove', agentChanges: { remove: [agent.id] } })
    expect(engine.agents.size).toBe(0)
    engine.unmount()
  })
})
it('ticker runs only when visible, moving and motion is allowed, and stops on teardown', () => {
  const start = vi.fn(),
    stop = vi.fn(),
    controller = new TickerController(start, stop)
  controller.update()
  expect(start).not.toHaveBeenCalled()
  controller.moving = true
  controller.update()
  expect(start).toHaveBeenCalledOnce()
  controller.visible = false
  controller.update()
  expect(controller.running).toBe(false)
  controller.visible = true
  controller.reducedMotion = true
  controller.update()
  expect(controller.running).toBe(false)
  controller.reducedMotion = false
  controller.update()
  expect(start).toHaveBeenCalledTimes(2)
  controller.destroy()
  expect(controller.running).toBe(false)
  expect(stop).toHaveBeenCalled()
})
it('movement speed changes actual distance while diagnostics report measured walking FPS and stop on unmount', async () => {
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
  Object.defineProperty(document, 'hidden', { configurable: true, value: false })
  let tick: ((delta: number, actualElapsed?: number) => void) | undefined
  const drawing = renderer()
  drawing.mount = vi.fn(async (_host, update) => {
    tick = update
  })
  const engine = new WorldEngine({ layout, renderer: drawing }),
    host = document.createElement('div')
  Object.defineProperties(host, { clientWidth: { value: 400 }, clientHeight: { value: 300 } })
  await engine.mount(host)
  engine.upsertAgent(agent)
  engine.applyEvent({
    id: 'move',
    timestamp: 1,
    agentId: agent.id,
    activity: 'read',
    status: 'started',
    target: { roomId: 'first' },
    text: 'Reading',
  })
  const start = engine.agents.get(agent.id)!.position.y
  tick!(100, 100)
  expect(engine.agents.get(agent.id)!.position.y - start).toBeCloseTo(16)
  engine.setSpeed(2)
  tick!(100, 100)
  expect(engine.agents.get(agent.id)!.position.y - start).toBeCloseTo(48)
  tick!(100, 100)
  tick!(100, 100)
  expect(window.__excaliburWorld?.running).toBe(true)
  expect(window.__excaliburWorld?.fps).toBeCloseTo(10)
  engine.unmount()
  expect(window.__excaliburWorld).toEqual({ running: false, fps: 0, mode: 'chat', ready: false })
})

it('exposes readiness after assets and steps a manual clock and camera independently of browser ticks', async () => {
  vi.stubGlobal('matchMedia', () => ({
    matches: false,
    addEventListener: vi.fn(),
    removeEventListener: vi.fn(),
  }))
  let browserTick: ((delta: number) => void) | undefined
  let finishAssets: (() => void) | undefined
  const drawing = renderer()
  drawing.mount = vi.fn(async (_host, tick) => {
    browserTick = tick
  })
  drawing.setLayout = vi.fn(
    () =>
      new Promise<void>((resolve) => {
        finishAssets = resolve
      }),
  )
  let elapsed = 0
  const engine = new WorldEngine({
    layout,
    renderer: drawing,
    manualClock: true,
    onAdvance: (ms) => {
      elapsed += ms
    },
    getElapsed: () => elapsed,
  })
  const host = document.createElement('div')
  Object.defineProperties(host, { clientWidth: { value: 400 }, clientHeight: { value: 300 } })
  const mounted = engine.mount(host)
  await Promise.resolve()
  expect(window.__excaliburWorld?.ready).toBe(false)
  finishAssets!()
  await mounted
  expect(window.__excaliburWorld?.ready).toBe(true)
  engine.focusPoint({ x: 700, y: 500 })
  browserTick!(1000)
  expect(engine.camera.x).toBe(0)
  window.__excaliburWorld!.advance!(55)
  expect(elapsed).toBe(55)
  expect(engine.camera.x).toBeGreaterThan(0)
  expect(engine.camera.x).toBeLessThan(800 - 400 / 0.75)
  const intermediate = { ...engine.camera }
  vi.advanceTimersByTime(5000)
  expect(engine.camera).toEqual(intermediate)
  window.__excaliburWorld!.settle!()
  expect(engine.camera.x).toBeCloseTo(800 - 400 / 0.75)
  expect(engine.camera.y).toBeCloseTo(200)
  expect(window.__excaliburWorld?.camera).toEqual(engine.camera)
  expect(window.__excaliburWorld?.elapsed).toBe(55)
  window.__excaliburWorld!.advance!(Number.NaN)
  window.__excaliburWorld!.advance!(-1)
  expect(elapsed).toBe(55)
  engine.unmount()
  expect(window.__excaliburWorld?.advance).toBeUndefined()
})

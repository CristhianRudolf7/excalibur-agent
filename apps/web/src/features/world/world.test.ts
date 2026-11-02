import { describe, expect, it } from 'vitest'
import { activityMap, behaviorFor, destinationFor } from './activity-map'
import { clampCamera, stepZoom } from './engine/camera'
import { hitPolygon, roomAt } from './layout'
import { createSimulation, worldSpeed } from './simulation'
import { createWorldStore } from './store'
import { campus } from './seed/layout'
import { agents, subagents } from './seed/agents'
import { missions } from './seed/missions'
import { initialHistory, initialDialog, makeTimeline, LOOP_DURATION } from './seed/timeline'
import { formatWorldTime } from './format'
import { filterActivity, type WorldEvent } from './events'
const event: WorldEvent = {
  id: 'event',
  agentId: 'nova',
  activity: 'read',
  target: { roomId: 'library', taskId: 'EXC-142' },
  status: 'started',
  timestamp: 1000,
  text: 'worldSeedRead',
}
const store = () => createWorldStore({ layout: campus, agents, missions })
describe('generic world event session', () => {
  it('queues important events in FIFO order, records history once and updates matching missions', () => {
    const session = store()
    session.getState().emit(event)
    session.getState().emit(event)
    session.getState().emit({ ...event, id: 'finished', status: 'completed', progress: 100 })
    expect(session.getState().queue.map((entry) => entry.id)).toEqual(['event', 'finished'])
    expect(session.getState().activity).toHaveLength(2)
    expect(session.getState().missions.find((mission) => mission.id === 'EXC-142')?.progress).toBe(
      100,
    )
    session.getState().advance()
    expect(session.getState().queue[0]?.id).toBe('finished')
    session.getState().advance()
    session.getState().advance()
    expect(session.getState().queue).toEqual([])
    session.getState().reopen(event)
    expect(session.getState().queue).toEqual([event])
  })
  it('accepts a new activity solely by extending the behavior table', () => {
    const table = {
      ...activityMap,
      translate: { ...activityMap.write_doc!, roomType: 'project', spot: 'desk', dialog: true },
    }
    const session = createWorldStore({ layout: campus, agents, missions }, table)
    const custom = { ...event, activity: 'translate', target: { projectId: 'launch' } }
    session.getState().emit(custom)
    expect(session.getState().queue[0]?.target.roomId).toBe('launch')
    expect(destinationFor(custom, campus, table)?.room.type).toBe('project')
    expect(behaviorFor(custom, table).animation).toBe('write')
    expect(
      destinationFor({ ...custom, target: { projectId: 'observatory' } }, campus, table)?.room.id,
    ).toBe('observatory')
    expect(behaviorFor({ ...custom, activity: 'unknown' }).animation).toBe('idle')
  })
  it('adds/removes arbitrary agents at runtime and keeps their identity in past activity', () => {
    const session = store(),
      helper = { ...subagents[0]!, id: 'arbitrary-helper', name: 'Helper' }
    session.getState().emit({ ...event, agentChanges: { upsert: [helper] } })
    expect(session.getState().agents[helper.id]).toEqual(helper)
    session.getState().emit({ ...event, id: 'remove', agentChanges: { remove: [helper.id] } })
    expect(session.getState().agents[helper.id]).toBeUndefined()
    expect(session.getState().identities[helper.id]).toEqual(helper)
  })
  it('filters room activity without coupling to a particular layout', () => {
    expect(filterActivity([event], 'library')).toEqual([event])
    expect(filterActivity([event], 'elsewhere')).toEqual([])
    expect(filterActivity([event])).toEqual([event])
  })
})
describe('rich deterministic seed producer', () => {
  it('has eight configured rooms, one primary agent, four helpers, missions and deliverables', () => {
    expect(campus.rooms).toHaveLength(8)
    expect(agents).toHaveLength(1)
    expect(agents[0]?.id).toBe('excalibur')
    expect(subagents).toHaveLength(4)
    for (const room of campus.rooms) {
      expect(room.deliverables.length).toBeGreaterThan(0)
      expect(missions.some((mission) => mission.roomId === room.id)).toBe(true)
    }
    expect(campus.rooms.filter((room) => !room.background)).toHaveLength(2)
    expect(initialHistory(1000000)).toHaveLength(15)
    expect(initialDialog(1000000).timestamp).toBe(1000000)
  })
  it('uses every activity kind and repeats events with unique IDs and stable timestamps', () => {
    const script = makeTimeline(3)
    expect(script.length).toBeGreaterThanOrEqual(40)
    expect(new Set(script.map((entry) => entry.event.activity))).toEqual(
      new Set(Object.keys(activityMap)),
    )
    const a = createSimulation(script, LOOP_DURATION, 10000),
      b = createSimulation(makeTimeline(3), LOOP_DURATION, 10000)
    expect(a.advance(LOOP_DURATION)).toEqual(b.advance(LOOP_DURATION))
    const next = a.advance(LOOP_DURATION)
    expect(next[0]?.id).toBe('1:script-0')
    expect(next[0]?.timestamp).toBe(10000 + LOOP_DURATION + 1500)
    expect(next.some((event) => event.agentChanges?.upsert?.length)).toBe(true)
    expect(next.some((event) => event.agentChanges?.remove?.length)).toBe(true)
    expect(a.advance(0)).toEqual([])
  })
  it('can step with different clock sizes without dropping or duplicating events', () => {
    const script = makeTimeline(),
      a = createSimulation(script, LOOP_DURATION, 0),
      b = createSimulation(script, LOOP_DURATION, 0)
    const small = Array.from({ length: 100 }, () => a.advance(500)).flat()
    expect(small).toEqual(b.advance(50000))
    expect(worldSpeed('?worldSpeed=4', true)).toBe(4)
    expect(worldSpeed('?worldSpeed=4', false)).toBe(1)
    expect(worldSpeed('?worldSpeed=NaN', true)).toBe(1)
    expect(worldSpeed('?worldSpeed=100', true)).toBe(16)
  })
})
describe('generic camera, geometry and time', () => {
  it('clamps using the supplied map bounds and discrete zoom steps', () => {
    expect(clampCamera({ x: -40, y: 9000, zoom: 1 }, 1000, 800, campus)).toEqual({
      x: 0,
      y: 800,
      zoom: 1,
    })
    expect(clampCamera({ x: 9, y: 9, zoom: 1 }, 1000, 800, { width: 20, height: 10 })).toEqual({
      x: 0,
      y: 0,
      zoom: 1,
    })
    expect(stepZoom(0.75, -1)).toBe(0.75)
    expect(stepZoom(2, 1)).toBe(1.5)
    expect(stepZoom(1, 1)).toBe(1.25)
  })
  it('hit-tests measured and configuration-only room polygons', () => {
    for (const room of campus.rooms) {
      const center = {
        x: room.polygon.reduce((sum, point) => sum + point.x, 0) / room.polygon.length,
        y: room.polygon.reduce((sum, point) => sum + point.y, 0) / room.polygon.length,
      }
      expect(hitPolygon(center, room.polygon)).toBe(true)
      expect(roomAt(campus, center)?.id).toBe(room.id)
    }
    expect(roomAt(campus, { x: -1, y: -1 })).toBeUndefined()
  })
  it('formats Spanish times in 24 hours and honors the active English locale', () => {
    const date = new Date(2026, 10, 1, 18, 46).getTime()
    expect(formatWorldTime(date, 'es')).toBe('18:46')
    expect(formatWorldTime(date, 'en-US')).toMatch(/06:46.*PM/)
  })
})

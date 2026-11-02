import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { groupArtifacts, receiveArtifactEvent, useArtifacts } from './store'
import type { Artifact, ArtifactEvent } from './types'
export const artifact: Artifact = {
  id: 'overview',
  kind: 'diagram',
  title: 'Overview',
  format: 'svg',
  mime: 'image/svg+xml',
  size: 100,
  created_at: '2026-11-02T10:00:00Z',
  group: 'architecture',
}
const ready = (turn = 'turn-1'): ArtifactEvent => ({
  type: 'artifact.ready',
  payload: { session_id: 'active', turn_id: turn, artifacts: [artifact] },
})
afterEach(() => vi.restoreAllMocks())
beforeEach(() => {
  localStorage.clear()
  useArtifacts.setState({ autoOpen: true, conversations: {} })
})
it('opens once per turn, preserving the dismissed selection on replay and remount', () => {
  const open = vi.fn()
  receiveArtifactEvent(ready(), 'active', open)
  useArtifacts.getState().showAll('active')
  receiveArtifactEvent(ready(), 'active', open)
  expect(open).toHaveBeenCalledTimes(1)
  expect(useArtifacts.getState().conversations.active?.selectedTurn).toBeUndefined()
  receiveArtifactEvent(ready('turn-2'), 'active', open)
  expect(open).toHaveBeenCalledTimes(2)
  expect(useArtifacts.getState().conversations.active?.selectedTurn).toBe('turn-2')
})
it('respects the persisted auto-open setting, including replay after enabling it', async () => {
  const open = vi.fn()
  useArtifacts.getState().setAutoOpen(false)
  await useArtifacts.persist.rehydrate()
  expect(useArtifacts.getState().autoOpen).toBe(false)
  receiveArtifactEvent(ready(), 'active', open)
  expect(useArtifacts.getState().conversations.active?.items).toHaveLength(1)
  useArtifacts.getState().setAutoOpen(true)
  receiveArtifactEvent(ready(), 'active', open)
  expect(open).not.toHaveBeenCalled()
})
it('ignores another conversation and empty turn results', () => {
  const open = vi.fn()
  receiveArtifactEvent(ready(), 'another', open)
  expect(useArtifacts.getState().conversations).toEqual({})
  receiveArtifactEvent(
    { ...ready(), payload: { ...ready().payload, artifacts: [] } },
    'active',
    open,
  )
  expect(open).not.toHaveBeenCalled()
})
it('groups overview and detail in production order, keeping turns and ungrouped images separate', () => {
  const items = [
    { ...artifact, id: 'detail', created_at: '2026-11-02T10:01:00Z', turn_id: 'one' },
    { ...artifact, turn_id: 'one' },
    { ...artifact, id: 'next-turn', turn_id: 'two' },
    { ...artifact, id: 'single', group: undefined, turn_id: 'one' },
  ]
  const groups = groupArtifacts(items)
  expect(groups.map((group) => group.artifacts.map((item) => item.id))).toEqual([
    ['overview', 'detail'],
    ['next-turn'],
    ['single'],
  ])
  expect(items[0]?.id).toBe('detail')
})

it('tracks created artifacts once without opening and clears progress when the turn is ready', () => {
  const open = vi.fn()
  const created: ArtifactEvent = {
    type: 'artifact.created',
    payload: { session_id: 'active', turn_id: 'turn-1', artifact },
  }
  receiveArtifactEvent(created, 'active', open)
  receiveArtifactEvent(created, 'active', open)
  receiveArtifactEvent(
    { ...created, payload: { ...created.payload, artifact: { ...artifact, id: 'detail' } } },
    'active',
    open,
  )
  expect(open).not.toHaveBeenCalled()
  expect(useArtifacts.getState().conversations.active).toMatchObject({ pendingTurns: ['turn-1'] })
  expect(useArtifacts.getState().conversations.active?.items).toHaveLength(2)
  receiveArtifactEvent(ready(), 'active', open)
  expect(useArtifacts.getState().conversations.active?.pendingTurns).toEqual([])
  receiveArtifactEvent(created, 'active', open)
  expect(useArtifacts.getState().conversations.active?.pendingTurns).toEqual([])
  expect(open).toHaveBeenCalledTimes(1)
})

it('orders artifact timestamps chronologically across time zones', () => {
  const earlier = { ...artifact, id: 'earlier', created_at: '2026-11-02T10:30:00+01:00' }
  receiveArtifactEvent(
    {
      type: 'artifact.ready',
      payload: { session_id: 'active', turn_id: 'one', artifacts: [earlier, artifact] },
    },
    'active',
    () => {},
  )
  const items = useArtifacts.getState().conversations.active!.items
  expect(items.map((item) => item.id)).toEqual(['overview', 'earlier'])
  expect(groupArtifacts(items)[0]?.artifacts[0]?.id).toBe('earlier')
})
it('does not combine an ungrouped artifact with a group sharing its ID', () => {
  expect(
    groupArtifacts([
      { ...artifact, id: 'same', group: undefined, turn_id: 'one' },
      { ...artifact, id: 'other', group: 'same', turn_id: 'one' },
    ]),
  ).toHaveLength(2)
})

it('keeps event handling usable when browser persistence is full or disabled', () => {
  vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
    throw new Error('Quota exceeded')
  })
  const open = vi.fn()
  expect(() => receiveArtifactEvent(ready(), 'active', open)).not.toThrow()
  expect(open).toHaveBeenCalledTimes(1)
  expect(useArtifacts.getState().conversations.active?.items).toHaveLength(1)
})

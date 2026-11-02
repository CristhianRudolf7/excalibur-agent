import { afterEach, expect, it } from 'vitest'
import { useChat } from '@/features/chat/store'
import { useTasks } from '@/features/tasks/store'
import { getTasks } from '@/features/tasks/mock-data'
import { connectChat } from './chat-bridge'
import { createWorldStore } from './store'
import { campus } from './seed/layout'
import { agents } from './seed/agents'
import { missions } from './seed/missions'
import type { WorldEvent } from './events'
afterEach(() => {
  useChat.setState({ live: {} })
  useTasks.setState({ tasks: getTasks() })
})
it('keeps visualization seeds out of chat and mirrors live replies once', () => {
  const store = createWorldStore({ layout: campus, agents, missions })
  const disconnect = connectChat(store, () => 'es')
  const timestamp = new Date(2026, 10, 1, 18, 46).getTime()
  const event: WorldEvent = {
    id: 'reply',
    agentId: 'excalibur',
    activity: 'reply',
    status: 'progress',
    text: 'A real reply',
    literal: true,
    target: { roomId: 'code' },
    timestamp,
  }
  store.getState().emit(event)
  expect(useChat.getState().live.excalibur).toBeUndefined()
  useChat.getState().merge('excalibur', [
    {
      id: 'world-persisted-message',
      sender: 'agent',
      text: 'Another reply',
      time: '18:47',
      literal: true,
    },
  ])
  expect(useChat.getState().live.excalibur).toHaveLength(1)
  expect(store.getState().queue.at(-1)?.chatMessageId).toBe('world-persisted-message')
  expect(store.getState().activity[0]?.text).toBe('Another reply')
  disconnect()
})
it('updates the shared task matching the event target without scenario-specific event IDs', () => {
  const store = createWorldStore({ layout: campus, agents, missions }),
    disconnect = connectChat(store, () => 'es')
  store.getState().emit({
    id: 'arbitrary-completion',
    agentId: 'excalibur',
    activity: 'run_command',
    status: 'completed',
    text: 'testsPassed',
    target: { taskId: 'EXC-143' },
    timestamp: Date.now(),
  })
  expect(useTasks.getState().tasks.find((task) => task.id === 'EXC-143')?.status).toBe('done')
  disconnect()
})

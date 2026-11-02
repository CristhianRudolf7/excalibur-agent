import { useChat } from '@/features/chat/store'
import { useTasks } from '@/features/tasks/store'
import type { WorldStore } from './store'
/** Mirrors live assistant replies into the visualization; seed events never enter real chat. */
export function connectChat(store: WorldStore, _locale: () => string) {
  const world = store.subscribe((state, previous) => {
    const event = state.activity[0]
    if (state.activity === previous.activity || !event) return
    if (event.status === 'completed' && event.target.taskId)
      useTasks.getState().complete(event.target.taskId)
  })
  const chat = useChat.subscribe((state, previous) => {
    for (const [id, conversation] of Object.entries(state.live)) {
      const message = conversation.at(-1)
      if (conversation === previous.live[id] || !message || message.sender !== 'agent') continue
      const agent = store.getState().agents[id]
      if (agent)
        store.getState().emit({
          id: `chat-${id}-${message.id}`,
          agentId: id,
          activity: 'reply',
          target: { roomId: agent.homeRoomId },
          status: 'progress',
          text: message.text,
          literal: message.literal,
          timestamp: Date.now(),
          chatMessageId: message.id,
        })
    }
  })
  return () => {
    world()
    chat()
  }
}

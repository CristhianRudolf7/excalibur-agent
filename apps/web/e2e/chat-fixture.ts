import { defaultAgent } from '../src/features/agents/store'
import type { Page } from '@playwright/test'
import type { ConversationMessage } from '../src/lib/rpc/contract'
import { mockEvents } from './event-fixture'

export async function mockChat(page: Page, initial: Partial<ConversationMessage>[] = []) {
  const messages = initial.map((message, index) => ({
    id: `msg-${index}`,
    conversation_id: 'cnv_web_excalibur',
    role: 'assistant',
    text: '',
    ...message,
  }))
  const events = await mockEvents(page)
  const emit = events.emit
  await page.route('**/rpc', async (route) => {
    const request = route.request().postDataJSON()
    if (request.method === 'agents.list')
      return route.fulfill({
        json: {
          jsonrpc: '2.0',
          id: request.id,
          result: { agents: [defaultAgent], favorite_id: defaultAgent.id },
        },
      })
    if (request.method === 'agents.mark_read')
      return route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result: { unread: 0 } } })
    if (request.method === 'credentials.pending')
      return route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result: { requests: [] } } })
    if (request.method === 'activity.list')
      return route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result: { items: [] } } })
    if (request.method === 'conversation.history')
      return route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result: { messages } } })
    if (request.method === 'conversation.send') {
      const id = `sent-${messages.length}`
      const message = {
        id,
        conversation_id: 'cnv_web_excalibur',
        role: 'user',
        text: request.params.text,
      }
      messages.push(message)
      // Echo before the receipt deliberately exercises optimistic message reconciliation.
      emit('message.created', { ...message, message_id: id })
      return route.fulfill({ json: { jsonrpc: '2.0', id: request.id, result: { msg_id: id } } })
    }
    return route.fallback()
  })
  return {
    emit,
    connected: () => events.connected('message.created'),
    messages,
    disconnect: () => events.disconnect('message.created'),
    pause: () => events.pause('message.created'),
    resume: () => events.resume('message.created'),
  }
}

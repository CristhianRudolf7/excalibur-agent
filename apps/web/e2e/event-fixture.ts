import { expect, type Page, type WebSocketRoute } from '@playwright/test'

function createEventBus() {
  const subscriptions = new Map<WebSocketRoute, string[]>()
  let sequence = 0
  return {
    connect(socket: WebSocketRoute) {
      socket.onClose(() => subscriptions.delete(socket))
      socket.onMessage((data) => {
        const request = JSON.parse(String(data))
        if (request.method !== 'conversation.subscribe') return
        subscriptions.set(socket, request.params.types)
        socket.send(
          JSON.stringify({ jsonrpc: '2.0', id: request.id, result: { events: [], cursor: '' } }),
        )
      })
    },
    emit(type: string, payload: unknown) {
      const event = {
        seq: ++sequence,
        id: `event-${sequence}`,
        type,
        at: new Date().toISOString(),
        payload,
      }
      for (const [socket, types] of subscriptions)
        if (types.includes(type))
          socket.send(JSON.stringify({ jsonrpc: '2.0', method: 'event', params: event }))
    },
    connected: (type: string) => [...subscriptions.values()].some((types) => types.includes(type)),
    async ready(type: string) {
      await expect
        .poll(() => [...subscriptions.values()].some((types) => types.includes(type)))
        .toBe(true)
    },
    disconnect(type: string) {
      for (const [socket, types] of subscriptions) {
        if (!types.includes(type)) continue
        subscriptions.delete(socket)
        socket.close({ code: 1012, reason: 'Restart' })
      }
    },
  }
}

const buses = new WeakMap<Page, ReturnType<typeof createEventBus>>()

/** All feature mocks share one daemon bus, with independent subscription filters. */
export async function mockEvents(page: Page) {
  let bus = buses.get(page)
  if (!bus) {
    bus = createEventBus()
    buses.set(page, bus)
    await page.routeWebSocket('**/ws', bus.connect)
  }
  return bus
}

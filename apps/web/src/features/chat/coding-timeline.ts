import type { CodingRun } from './coding-store'
import type { Message } from './types'

export const activeCodingStates = ['created', 'queued', 'running', 'verifying', 'recovering']
export const codingTurn = (run: CodingRun) => run.turn ?? run.parent ?? run.id

export type ChatRow =
  | { id: string; message: Message; turn?: never; runs?: never; notices?: never }
  | { id: string; turn: string; runs: CodingRun[]; notices: Message[]; message?: never }

// Persisted message IDs contain the creation time. This also places replayed
// activity correctly when history loads after the event subscription.
function messageTime(id: string): number | undefined {
  const ulid = /^msg_([0-9A-HJKMNP-TV-Z]{26})$/i.exec(id)?.[1]
  if (!ulid) return
  const alphabet = '0123456789ABCDEFGHJKMNPQRSTVWXYZ'
  return [...ulid.slice(0, 10).toUpperCase()].reduce(
    (time, char) => time * 32 + alphabet.indexOf(char),
    0,
  )
}

export function codingTimeline(messages: Message[], runs: CodingRun[]): ChatRow[] {
  const turns = new Map<string, CodingRun[]>()
  for (const run of runs) {
    const turn = codingTurn(run)
    turns.set(turn, [...(turns.get(turn) ?? []), run])
  }
  const anchored = new Map<string | undefined, ChatRow[]>()
  const folded = new Set<string>()
  for (const [turn, group] of turns) {
    const started = Math.min(...group.map((run) => Date.parse(run.started)))
    const user = messages.findLast(
      (message) => message.sender === 'user' && (messageTime(message.id) ?? Infinity) <= started,
    )
    const anchor = user?.id ?? group.find((run) => run.afterMessage)?.afterMessage
    const key = messages.some((message) => message.id === anchor) ? anchor : undefined
    const start = messages.findIndex((message) => message.id === key)
    const next = messages.findIndex((message, index) => index > start && message.sender === 'user')
    const replies =
      start < 0
        ? []
        : messages
            .slice(start + 1, next < 0 ? undefined : next)
            .filter((message) => message.sender === 'agent')
    // Intermediate notices stay with the work in its bounded feed; the final
    // reply returns to the chat timeline once all work in the turn finishes.
    const running = group.some((run) => activeCodingStates.includes(run.state))
    const notices = running ? replies : replies.slice(0, -1)
    for (const notice of notices) folded.add(notice.id)
    anchored.set(key, [
      ...(anchored.get(key) ?? []),
      { id: `coding:${turn}`, turn, runs: group, notices },
    ])
  }
  return [
    ...(anchored.get(undefined) ?? []),
    ...messages
      .filter((message) => !folded.has(message.id))
      .flatMap((message): ChatRow[] => [
        { id: message.id, message },
        ...(anchored.get(message.id) ?? []),
      ]),
  ]
}

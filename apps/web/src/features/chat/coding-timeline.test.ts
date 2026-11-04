import { expect, it } from 'vitest'
import { codingTimeline } from './coding-timeline'
import type { CodingRun } from './coding-store'
import type { Message } from './types'

const message = (id: string, sender: Message['sender']): Message => ({
  id,
  sender,
  text: id,
  time: '',
})
const run = (id: string, afterMessage?: string, parent?: string): CodingRun => ({
  id,
  turn: 'turn',
  parent,
  afterMessage,
  state: 'completed',
  started: '2026-11-04T12:00:00Z',
  updated: '2026-11-04T12:00:00Z',
  steps: [],
  done: 0,
  entries: [],
  omitted: 0,
})
it('groups nested runs in one stable turn before notices and the final answer', () => {
  const rows = codingTimeline(
    [
      message('user', 'user'),
      message('notice', 'agent'),
      message('final', 'agent'),
      message('next', 'user'),
    ],
    [run('parent', 'user'), run('child', 'user', 'parent'), run('grandchild', 'user', 'child')],
  )
  expect(rows.map((row) => row.id)).toEqual(['user', 'coding:turn', 'final', 'next'])
  expect(rows[1]!.runs).toHaveLength(3)
  expect(rows[1]!.notices?.map((notice) => notice.id)).toEqual(['notice'])
})
it('places replay by persisted message time even when later history was loaded first', () => {
  const id = (time: number) => {
    const alphabet = '0123456789ABCDEFGHJKMNPQRSTVWXYZ'
    let encoded = ''
    for (let i = 0; i < 10; i++, time = Math.floor(time / 32))
      encoded = alphabet[time % 32] + encoded
    return `msg_${encoded}${'0'.repeat(16)}`
  }
  const first = id(Date.parse('2026-11-04T11:00:00Z'))
  const last = id(Date.parse('2026-11-04T13:00:00Z'))
  const rows = codingTimeline(
    [message(first, 'user'), message('final', 'agent'), message(last, 'user')],
    [run('root', last)],
  )
  expect(rows.map((row) => row.id)).toEqual([first, 'coding:turn', 'final', last])
})
it('shows recovered activity before available replies when its user is not loaded', () => {
  expect(
    codingTimeline([message('final', 'agent')], [run('root', 'older')]).map((row) => row.id),
  ).toEqual(['coding:turn', 'final'])
})

it('folds live notices into the bounded turn and restores only the final reply on completion', () => {
  const messages = [message('user', 'user'), message('notice', 'agent'), message('answer', 'agent')]
  const running = { ...run('root', 'user'), state: 'running' }
  const live = codingTimeline(messages, [running])
  expect(live.map((row) => row.id)).toEqual(['user', 'coding:turn'])
  expect(live[1]!.notices?.map((notice) => notice.id)).toEqual(['notice', 'answer'])
  const done = codingTimeline(messages, [{ ...running, state: 'completed' }])
  expect(done.map((row) => row.id)).toEqual(['user', 'coding:turn', 'answer'])
  expect(done[1]!.notices?.map((notice) => notice.id)).toEqual(['notice'])
})

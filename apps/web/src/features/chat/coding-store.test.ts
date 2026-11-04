import { beforeEach, expect, it } from 'vitest'
import { useCodingActivity, type CodingPayload } from './coding-store'
import type { ConversationEvent } from '@/lib/rpc/contract'

export function codingEvent(type: string, payload: Partial<CodingPayload> = {}): ConversationEvent {
  return {
    type,
    seq: 1,
    id: 'event',
    at: new Date().toISOString(),
    payload: {
      conversation_id: 'conversation',
      run_id: 'child',
      parent_run_id: 'parent',
      turn_id: 'turn',
      ...payload,
    },
  }
}
beforeEach(() => useCodingActivity.setState({ conversations: {} }))
it('updates stable rows, deduplicates replay, and keeps conversations isolated', () => {
  const receive = useCodingActivity.getState().receive
  receive(
    codingEvent('tool.called', {
      call_id: 'read',
      tool: 'read_file',
      kind: 'read',
      path: 'main.go',
      status: 'running',
    }),
  )
  const first = useCodingActivity.getState().conversations.conversation![0]!
  receive(
    codingEvent('tool.completed', { call_id: 'read', status: 'completed', output: 'contents' }),
  )
  receive(codingEvent('tool.called', { call_id: 'read', status: 'running' }))
  receive(
    codingEvent('file.edited', {
      call_id: 'edit',
      edit_id: 'version',
      path: 'main.go',
      diff: '-old\n+new',
    }),
  )
  receive(
    codingEvent('file.edited', {
      call_id: 'edit',
      edit_id: 'version',
      path: 'main.go',
      diff: '-old\n+new',
    }),
  )
  receive(codingEvent('tool.called', { conversation_id: 'other', run_id: 'other' }))
  receive(codingEvent('tool.called', { conversation_id: undefined, run_id: 'unscoped' }))
  const runs = useCodingActivity.getState().conversations.conversation!
  expect(runs).toHaveLength(1)
  expect(runs[0]).toMatchObject({ parent: 'parent', turn: 'turn', started: first.started })
  expect(runs[0]!.entries).toHaveLength(2)
  expect(runs[0]!.entries[0]).toMatchObject({
    id: 'call:read',
    status: 'completed',
    path: 'main.go',
    output: 'contents',
  })
  expect(Object.keys(useCodingActivity.getState().conversations)).toEqual(['conversation', 'other'])
})
it('merges session polling, preserves commands, and stops spinners on terminal progress', () => {
  const receive = useCodingActivity.getState().receive
  receive(
    codingEvent('tool.completed', {
      call_id: 'launch',
      tool: 'exec',
      command: 'go test -v ./...',
      session_id: 'session',
      status: 'running',
      output: 'building',
    }),
  )
  receive(
    codingEvent('tool.completed', {
      call_id: 'poll',
      tool: 'exec_io',
      session_id: 'session',
      status: 'completed',
      output: '--- PASS: TestOne',
      exit_code: 0,
    }),
  )
  let run = useCodingActivity.getState().conversations.conversation![0]!
  expect(run.entries).toHaveLength(1)
  expect(run.entries[0]).toMatchObject({
    id: 'call:launch',
    command: 'go test -v ./...',
    output: 'building\n--- PASS: TestOne',
    exit_code: 0,
  })
  receive(codingEvent('tool.called', { call_id: 'next', status: 'running' }))
  receive(
    codingEvent('run.progress', {
      state: 'failed',
      steps: ['Read', 'Edit'],
      plan_done: 0,
      elapsed_ms: 5000,
    }),
  )
  run = useCodingActivity.getState().conversations.conversation![0]!
  expect(run).toMatchObject({ state: 'failed', steps: ['Read', 'Edit'], elapsed: 5000 })
  expect(run.entries[1]!.status).toBe('failed')
})
it('bounds retained entries and runs', () => {
  const receive = useCodingActivity.getState().receive
  for (let i = 0; i < 210; i++) receive(codingEvent('tool.called', { call_id: `call-${i}` }))
  expect(useCodingActivity.getState().conversations.conversation![0]).toMatchObject({ omitted: 10 })
  expect(useCodingActivity.getState().conversations.conversation![0]!.entries).toHaveLength(200)
  for (let i = 0; i < 15; i++)
    receive(codingEvent('run.progress', { run_id: `run-${i}`, state: 'completed' }))
  expect(useCodingActivity.getState().conversations.conversation).toHaveLength(12)
  expect(
    useCodingActivity.getState().conversations.conversation!.some((run) => run.id === 'child'),
  ).toBe(true)
})

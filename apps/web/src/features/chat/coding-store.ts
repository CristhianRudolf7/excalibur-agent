import { create } from 'zustand'
import type { ConversationEvent } from '@/lib/rpc/contract'

export interface CodingPayload {
  conversation_id: string
  run_id: string
  parent_run_id?: string
  turn_id?: string
  call_id?: string
  edit_id?: string
  tool?: string
  kind?: string
  name?: string
  state?: string
  status?: string
  path?: string
  command?: string
  session_id?: string
  output?: string
  diff?: string
  added?: number
  removed?: number
  binary?: boolean
  truncated?: boolean
  exit_code?: number
  elapsed_ms?: number
  steps?: string[]
  plan_done?: number
  plan_total?: number
}
export interface CodingEntry extends CodingPayload {
  sequence?: number
  id: string
  at: string
  type: 'tool' | 'edit'
}
export interface CodingRun {
  sequence?: number
  id: string
  parent?: string
  turn?: string
  afterMessage?: string
  name?: string
  state: string
  started: string
  updated: string
  elapsed?: number
  steps: string[]
  done: number
  entries: CodingEntry[]
  omitted: number
}
const maxRuns = 12
const maxEntries = 200
export const useCodingActivity = create<{
  conversations: Record<string, CodingRun[]>
  receive: (event: ConversationEvent, afterMessage?: string) => void
}>((set) => ({
  conversations: {},
  receive: (event, afterMessage) =>
    set((state) => {
      if (
        ![
          'tool.called',
          'tool.completed',
          'file.edited',
          'run.progress',
          'run.state_changed',
        ].includes(event.type)
      )
        return state
      const p = event.payload as CodingPayload
      // Coding activity requires explicit conversation attribution. System events
      // without it must never leak into whichever chat happens to be open.
      if (!p.conversation_id || !p.run_id) return state
      const runs = [...(state.conversations[p.conversation_id] ?? [])]
      const index = runs.findIndex((run) => run.id === p.run_id)
      const previous = runs[index]
      if (!previous && event.type.startsWith('run.') && !p.parent_run_id && !p.steps?.length)
        return state
      const run: CodingRun = previous
        ? { ...previous, entries: [...previous.entries] }
        : {
            id: p.run_id,
            sequence: event.seq,
            parent: p.parent_run_id,
            turn: p.turn_id,
            afterMessage:
              runs.find(
                (item) => (p.turn_id && item.turn === p.turn_id) || item.id === p.parent_run_id,
              )?.afterMessage ?? afterMessage,
            state: 'running',
            started: event.at,
            updated: event.at,
            steps: [],
            done: 0,
            entries: [],
            omitted: 0,
          }
      run.updated = event.at
      run.parent = p.parent_run_id ?? run.parent
      run.turn = p.turn_id ?? run.turn
      if (event.type.startsWith('run.')) {
        run.name = p.name ?? run.name
        run.state = p.state ?? run.state
        run.steps = p.steps?.slice(0, 20) ?? run.steps
        run.done = p.plan_done ?? run.done
        run.elapsed = p.elapsed_ms ?? run.elapsed
        // A terminal run cannot retain an indefinitely spinning command.
        if (['completed', 'failed', 'canceled', 'cancelled'].includes(run.state)) {
          run.entries = run.entries.map((entry) =>
            entry.status === 'running'
              ? { ...entry, status: run.state === 'completed' ? 'completed' : 'failed' }
              : entry,
          )
        }
      } else {
        const edit = event.type === 'file.edited'
        if (edit && p.call_id)
          run.entries = run.entries.filter(
            (entry) =>
              !(entry.type === 'tool' && entry.kind === 'edit' && entry.call_id === p.call_id),
          )
        const id = edit ? `edit:${p.edit_id ?? event.id}` : `call:${p.call_id ?? event.id}`
        // Polling an exec session updates its original command row in place.
        const found = run.entries.findIndex(
          (entry) =>
            entry.id === id ||
            (!edit && p.session_id && entry.type === 'tool' && entry.session_id === p.session_id),
        )
        const old = run.entries[found]
        const entry: CodingEntry = {
          ...old,
          ...p,
          id: old?.id ?? id,
          at: old?.at ?? event.at,
          sequence: old?.sequence ?? event.seq,
          type: edit ? 'edit' : 'tool',
        }
        if (old?.command && !p.command) entry.command = old.command
        if (old?.output && p.output && p.tool === 'exec_io')
          entry.output = `${old.output}\n${p.output}`.slice(-8192)
        // A late replay of a start must not reset a completed row.
        if (event.type === 'tool.called' && old && old.status !== 'running') return state
        if (found < 0) run.entries.push(entry)
        else run.entries[found] = entry
        if (run.entries.length > maxEntries) {
          run.omitted += run.entries.length - maxEntries
          run.entries = run.entries.slice(-maxEntries)
        }
      }
      if (index < 0) runs.push(run)
      else runs[index] = run
      // Retain running work preferentially when a conversation has many turns.
      while (runs.length > maxRuns) {
        const terminal = runs.findIndex(
          (item) => !['running', 'queued', 'paused', 'blocked'].includes(item.state),
        )
        runs.splice(terminal < 0 ? 0 : terminal, 1)
      }
      return { conversations: { ...state.conversations, [p.conversation_id]: runs } }
    }),
}))

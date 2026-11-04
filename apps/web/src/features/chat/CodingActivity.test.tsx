import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { CodingActivity } from './CodingActivity'
import { useCodingActivity, type CodingPayload } from './coding-store'

function ConversationActivity({ conversationId }: { conversationId: string }) {
  const runs = useCodingActivity((state) => state.conversations[conversationId])
  return runs ? <CodingActivity runs={runs} /> : null
}

const receive = (type: string, payload: Partial<CodingPayload> = {}) =>
  useCodingActivity.getState().receive({
    type,
    seq: 1,
    id: 'event',
    at: new Date().toISOString(),
    payload: {
      conversation_id: 'conversation',
      run_id: 'child',
      parent_run_id: 'parent',
      name: 'Workspace update',
      ...payload,
    },
  })
beforeEach(async () => {
  vi.spyOn(HTMLElement.prototype, 'offsetHeight', 'get').mockImplementation(function (
    this: HTMLElement,
  ) {
    return this.classList.contains('coding-scroll') ? 320 : 52
  })
  vi.spyOn(HTMLElement.prototype, 'offsetWidth', 'get').mockReturnValue(600)
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(320)
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(600)
  useCodingActivity.setState({ conversations: {} })
  await i18n.changeLanguage('en')
})
afterEach(() => {
  cleanup()
  document.documentElement.classList.remove('dark')
  vi.restoreAllMocks()
})

it.each(['light', 'dark'])('renders and expands edits and plans in the %s theme', (theme) => {
  document.documentElement.classList.toggle('dark', theme === 'dark')
  receive('run.progress', {
    state: 'running',
    steps: ['Read source', 'Update source', 'Run tests'],
    plan_done: 0,
  })
  receive('file.edited', {
    edit_id: 'version',
    call_id: 'edit',
    path: 'src/main.go',
    added: 2,
    removed: 1,
    diff: '@@ -1 +1 @@\n-old\n+new\n',
  })
  render(<ConversationActivity conversationId="conversation" />)
  expect(screen.getByRole('region', { name: 'Coding activity' })).toBeVisible()
  expect(screen.getByText('Workspace update')).toBeVisible()
  expect(screen.getByText('Running')).toBeVisible()
  expect(screen.getByText('+2')).toBeVisible()
  expect(screen.queryByText('+new')).not.toBeInTheDocument()
  const edit = screen.getByRole('button', { name: 'Edit: src/main.go' })
  expect(edit).toHaveAttribute('aria-expanded', 'false')
  fireEvent.click(edit)
  expect(edit).toHaveAttribute('aria-expanded', 'true')
  expect(screen.getByText('+new')).toBeVisible()
  fireEvent.click(edit)
  expect(screen.queryByText('+new')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Steps: 0 of 3' }))
  expect(screen.getByText('Run tests')).toBeVisible()
})
it('updates command status, summarizes tests and keeps final results visible', () => {
  receive('tool.called', {
    call_id: 'test',
    tool: 'exec',
    kind: 'command',
    command: 'go test -v ./...',
    status: 'running',
  })
  render(<ConversationActivity conversationId="conversation" />)
  act(() =>
    receive('tool.completed', {
      call_id: 'test',
      tool: 'exec',
      kind: 'command',
      command: 'go test -v ./...',
      status: 'failed',
      exit_code: 1,
      output: '--- PASS: TestOne\n--- FAIL: TestTwo\nFAIL',
    }),
  )
  expect(screen.getByText('1 passed')).toBeVisible()
  expect(screen.getByText('1 failed')).toBeVisible()
  expect(screen.getByText('Exit 1')).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Tests: go test -v ./...' }))
  expect(screen.getAllByText(/--- PASS: TestOne/)).toHaveLength(1)
  act(() => receive('run.progress', { state: 'failed', elapsed_ms: 6000 }))
  const summary = screen.getByRole('button', { name: /Coding activity · 1 action · 1 subagent/ })
  expect(summary).toHaveAttribute('aria-expanded', 'false')
  expect(screen.queryByRole('log')).not.toBeInTheDocument()
  fireEvent.click(summary)
  expect(summary).toHaveAttribute('aria-expanded', 'true')
  expect(screen.getByText('Failed')).toBeVisible()
  expect(screen.getByText('6 s')).toBeVisible()
  expect(screen.getByRole('status')).toHaveTextContent('Activity finished')
  fireEvent.click(summary)
  expect(screen.queryByRole('log')).not.toBeInTheDocument()
})
it('supports Spanish and explicitly displays bounded and binary previews', async () => {
  await i18n.changeLanguage('es')
  receive('file.edited', { edit_id: 'binary', path: 'image.png', binary: true, truncated: true })
  render(<ConversationActivity conversationId="conversation" />)
  expect(screen.getByRole('region', { name: 'Actividad de programación' })).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Editar: image.png' }))
  expect(screen.getByText('Archivo binario; vista previa no disponible.')).toBeVisible()
  expect(screen.getByText('Vista previa limitada por tamaño.')).toBeVisible()
})
it('does not render activity for another conversation', () => {
  receive('tool.called', { call_id: 'read', path: 'main.go' })
  const view = render(<ConversationActivity conversationId="other" />)
  expect(view.container).toBeEmptyDOMElement()
})

it('follows appends and delayed measurements, pauses on scroll up and resumes at the end', () => {
  const observed = new Map<Element, ResizeObserverCallback>()
  class Observer {
    callback: ResizeObserverCallback
    constructor(callback: ResizeObserverCallback) {
      this.callback = callback
    }
    observe(node: Element) {
      observed.set(node, this.callback)
    }
    unobserve(node: Element) {
      observed.delete(node)
    }
    disconnect() {}
  }
  vi.stubGlobal('ResizeObserver', Observer)
  receive('tool.called', { call_id: 'first', tool: 'exec', command: 'build' })
  render(<ConversationActivity conversationId="conversation" />)
  const feed = screen.getByRole('log')
  let height = 1000
  Object.defineProperty(feed, 'scrollHeight', { get: () => height })
  act(() => receive('tool.called', { call_id: 'second', tool: 'exec', command: 'verify' }))
  expect(feed.scrollTop).toBe(height)
  height = 1100
  act(() => observed.get(feed.firstElementChild!)?.([], {} as ResizeObserver))
  expect(feed.scrollTop).toBe(height)
  fireEvent.scroll(feed, { target: { scrollTop: 200 } })
  height = 1400
  act(() => receive('tool.called', { call_id: 'third', tool: 'exec', command: 'package' }))
  expect(feed.scrollTop).toBe(200)
  act(() => observed.get(feed.firstElementChild!)?.([], {} as ResizeObserver))
  expect(feed.scrollTop).toBe(200)
  fireEvent.scroll(feed, { target: { scrollTop: height - 320 } })
  height = 1500
  act(() => receive('tool.called', { call_id: 'fourth', tool: 'exec', command: 'finish' }))
  expect(feed.scrollTop).toBe(height)
  vi.unstubAllGlobals()
})

it('orders interleaved nested runs by event sequence when timestamps tie', () => {
  const push = (seq: number, type: string, payload: Partial<CodingPayload>) =>
    useCodingActivity.getState().receive({
      seq,
      id: `event-${seq}`,
      at: '2026-11-04T12:00:00Z',
      type,
      payload: { conversation_id: 'conversation', turn_id: 'root', run_id: 'root', ...payload },
    })
  push(1, 'run.progress', { state: 'running', steps: ['Build'] })
  push(2, 'run.progress', { run_id: 'child', parent_run_id: 'root', state: 'running' })
  push(3, 'tool.called', { call_id: 'first', command: 'first', tool: 'exec' })
  push(4, 'tool.called', {
    run_id: 'child',
    parent_run_id: 'root',
    call_id: 'second',
    command: 'second',
    tool: 'exec',
  })
  push(5, 'tool.called', { call_id: 'third', command: 'third', tool: 'exec' })
  const view = render(<ConversationActivity conversationId="conversation" />)
  expect(
    [...view.container.querySelectorAll('.coding-entry code')].map((node) => node.textContent),
  ).toEqual(['first', 'second', 'third'])
})

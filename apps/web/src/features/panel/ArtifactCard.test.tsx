import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { useArtifacts, receiveArtifactEvent } from '@/features/artifacts/store'
import { PanelHome } from './Panel'
afterEach(() => {
  cleanup()
  useArtifacts.setState({ conversations: {} })
})
it('updates the diagram card count while producing visuals without opening the panel', async () => {
  await i18n.changeLanguage('en')
  useArtifacts.setState({ conversations: {} })
  const onView = vi.fn(),
    open = vi.fn()
  render(
    <PanelHome
      session="active"
      data={{ items: [], changes: [], loading: false, error: false, retry: () => {} }}
      onView={onView}
    />,
  )
  const card = screen.getByRole('button', { name: /^Diagrams/ })
  expect(card).toHaveTextContent('0 diagrams')
  act(() => {
    for (const id of ['one', 'two'])
      receiveArtifactEvent(
        {
          type: 'artifact.created',
          payload: {
            session_id: 'active',
            turn_id: 'turn',
            artifact: {
              id,
              title: id,
              kind: 'diagram',
              format: 'svg',
              mime: 'image/svg+xml',
              size: 10,
              created_at: '',
            },
          },
        },
        'active',
        open,
      )
  })
  expect(card).toHaveTextContent('2 diagrams')
  expect(card.querySelector('.activity-dot')).not.toBeNull()
  expect(open).not.toHaveBeenCalled()
  expect(onView).not.toHaveBeenCalled()
  fireEvent.click(card)
  expect(onView).toHaveBeenCalledWith('diagrams')
  act(() => {
    receiveArtifactEvent(
      { type: 'artifact.ready', payload: { session_id: 'active', turn_id: 'turn', artifacts: [] } },
      'active',
      open,
    )
  })
  expect(card.querySelector('.activity-dot')).toBeNull()
})
it('previews recent conversation artifacts newest first and opens the complete history', async () => {
  await i18n.changeLanguage('en')
  useArtifacts.setState({ autoOpen: true, conversations: {} })
  for (const [turn, title, created_at] of [
    ['old', 'First diagram', '2026-11-01T10:00:00Z'],
    ['new', 'Latest diagram', '2026-11-02T10:00:00Z'],
  ]) {
    receiveArtifactEvent(
      {
        type: 'artifact.ready',
        payload: {
          session_id: 'active',
          turn_id: turn,
          artifacts: [
            {
              id: turn!,
              title: title!,
              kind: 'diagram',
              format: 'svg',
              mime: 'image/svg+xml',
              size: 20,
              created_at: created_at!,
            },
          ],
        },
      },
      'active',
      () => {},
    )
  }
  const onView = vi.fn()
  render(
    <PanelHome
      session="active"
      data={{ items: [], changes: [], loading: false, error: false, retry: () => {} }}
      onView={onView}
    />,
  )
  expect(screen.getAllByRole('listitem').map((node) => node.textContent)).toEqual([
    'Latest diagram',
    'First diagram',
  ])
  expect(useArtifacts.getState().conversations.active?.selectedTurn).toBe('new')
  fireEvent.click(screen.getByRole('button', { name: /^Diagrams/ }))
  expect(useArtifacts.getState().conversations.active?.selectedTurn).toBeUndefined()
  expect(onView).toHaveBeenCalledWith('diagrams')
})

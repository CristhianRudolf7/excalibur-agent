import { fireEvent, render, screen, cleanup } from '@testing-library/react'
import { afterEach, expect, it } from 'vitest'
import i18n from '@/lib/i18n'
import { PanelActivity, PanelChanges } from './Panel'
afterEach(cleanup)
it('filters a conversation with more than eight changed paths', async () => {
  await i18n.changeLanguage('en')
  const changes = Array.from({ length: 9 }, (_, index) => ({
    path: `src/file-${index}.ts`,
    status: 'modified' as const,
    added: 1,
    removed: 0,
    hunks: [],
  }))
  render(<PanelChanges changes={changes} />)
  expect(screen.getAllByRole('listitem')).toHaveLength(9)
  fireEvent.change(screen.getByRole('textbox', { name: 'Filter by path' }), {
    target: { value: 'FILE-8' },
  })
  expect(screen.getAllByRole('listitem')).toHaveLength(1)
  expect(screen.getByText('file-8.ts')).toBeVisible()
})
it('shows honest empty, loading and retry states', async () => {
  await i18n.changeLanguage('en')
  let retries = 0
  const data = {
    items: [],
    changes: [],
    loading: false,
    error: false,
    retry: () => {
      retries++
    },
  }
  const view = render(<PanelActivity data={data} />)
  expect(screen.getByText('Excalibur is not running subagents right now.')).toBeVisible()
  view.rerender(<PanelActivity data={{ ...data, loading: true }} />)
  expect(screen.getByRole('status')).toBeVisible()
  expect(
    screen.queryByText('Excalibur is not running subagents right now.'),
  ).not.toBeInTheDocument()
  view.rerender(<PanelActivity data={{ ...data, error: true }} />)
  expect(screen.getByRole('alert')).toHaveTextContent('Could not load activity.')
  fireEvent.click(screen.getByRole('button', { name: 'Retry' }))
  expect(retries).toBe(1)
})

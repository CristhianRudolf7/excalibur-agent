import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { useWhiteboard } from '@/features/whiteboard/store'
import { ArtifactActions } from './ArtifactActions'
import type { Artifact } from './types'
const artifact: Artifact = {
  id: 'one',
  kind: 'diagram',
  title: 'Architecture',
  format: 'svg',
  mime: 'image/svg+xml',
  size: 20,
  created_at: '',
}
const data = {
  id: artifact.id,
  mime: artifact.mime,
  content: '<svg/>',
  svg: '<svg/>',
  source: 'graph TD; A-->B',
  dialect: 'Mermaid',
  blob: new Blob(),
}
beforeEach(() => {
  void i18n.changeLanguage('en')
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useWhiteboard.setState({ board: undefined })
})
it('shows the source as inert text with its dialect, copies it and offers both downloads', async () => {
  const writeText = vi.fn().mockResolvedValue(undefined)
  Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
  render(<ArtifactActions artifact={artifact} data={data} url="blob:one" session="active" />)
  const source = screen.getByRole('button', { name: 'View source · Mermaid' })
  fireEvent.click(source)
  expect(source).toHaveAttribute('aria-expanded', 'true')
  expect(screen.getByText(data.source)).toBeVisible()
  expect(screen.getByRole('link', { name: 'Download SVG' })).toHaveAttribute(
    'download',
    'Architecture.svg',
  )
  expect(screen.getByRole('button', { name: 'Download PNG' })).toBeVisible()
  fireEvent.click(screen.getByRole('button', { name: 'Copy source' }))
  await waitFor(() => expect(screen.getByRole('status')).toHaveTextContent('Source copied'))
  expect(writeText).toHaveBeenCalledWith(data.source)
  expect(screen.queryByRole('button', { name: 'Open in whiteboard' })).toBeNull()
})
it('reports clipboard errors and hides source and import for binary artifacts without source', async () => {
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: { writeText: vi.fn().mockRejectedValue(new Error('Denied')) },
  })
  const view = render(
    <ArtifactActions artifact={artifact} data={data} url="blob:one" session="active" />,
  )
  fireEvent.click(screen.getByRole('button', { name: 'Copy source' }))
  expect(await screen.findByRole('alert')).toHaveTextContent('Could not complete the action.')
  view.rerender(
    <ArtifactActions
      artifact={{ ...artifact, format: 'png' }}
      data={{ ...data, source: undefined, svg: undefined }}
      url="blob:two"
      session="active"
    />,
  )
  expect(screen.queryByRole('button', { name: /View source/ })).toBeNull()
  expect(screen.queryByRole('button', { name: 'Open in whiteboard' })).toBeNull()
})
it('offers SVG import only for the conversation owning the existing whiteboard', () => {
  useWhiteboard.setState({
    board: {
      id: 'board',
      conversation_id: 'active',
      title: '',
      version: 1,
      updated_at: '',
      elements: [],
    },
  })
  const view = render(
    <ArtifactActions artifact={artifact} data={data} url="blob:one" session="active" />,
  )
  expect(screen.getByRole('button', { name: 'Open in whiteboard' })).toBeVisible()
  view.rerender(
    <ArtifactActions artifact={artifact} data={data} url="blob:one" session="another" />,
  )
  expect(screen.queryByRole('button', { name: 'Open in whiteboard' })).toBeNull()
})

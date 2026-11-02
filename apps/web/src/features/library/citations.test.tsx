import { afterEach, beforeEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import '@/lib/i18n'
import MarkdownBody from '@/components/markdown/MarkdownBody'
import { useViewerStore } from '@/features/viewer/store'
import { sourceUrl } from './source-url'
import { seedSources } from './seed'

beforeEach(() => {
  HTMLDialogElement.prototype.showModal = vi.fn(function (this: HTMLDialogElement) {
    this.setAttribute('open', '')
  })
  HTMLDialogElement.prototype.close = vi.fn()
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  useViewerStore.getState().close()
})
it('turns matching prose markers into buttons, preserving code, links and unmatched markers', () => {
  const { container } = render(
    <MarkdownBody sources={seedSources}>
      {
        'Evidence [1], **more [2]**, unknown [9]. `literal [1]`\n\n```txt\ncode [2]\n```\n\n[linked [1]](https://example.com)'
      }
    </MarkdownBody>,
  )
  expect(screen.getAllByRole('button', { name: /^Fuente \d/ })).toHaveLength(2)
  expect(container.querySelector('p')).toHaveTextContent('unknown [9]')
  expect(container.querySelector('code')).toHaveTextContent('literal [1]')
  expect(screen.getByRole('link', { name: 'linked [1]' })).toHaveTextContent('[1]')
  expect(container.querySelector('pre')).toHaveTextContent('code [2]')
})
it('leaves markers as text when no sources are supplied', () => {
  render(<MarkdownBody>{'Unsupported [1]'}</MarkdownBody>)
  expect(screen.queryByRole('button')).not.toBeInTheDocument()
})
it('opens document locations directly from markers and safe web URLs in a new tab', () => {
  const open = vi.spyOn(window, 'open').mockReturnValue(null)
  render(<MarkdownBody sources={seedSources}>{'See [1] and [2].'}</MarkdownBody>)
  fireEvent.click(screen.getByRole('button', { name: 'Fuente 1' }))
  expect(useViewerStore.getState().target).toMatchObject({
    documentId: 'demo-document-0',
    chunkId: 'demo-chunk-1',
  })
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Fuente 2' }))
  expect(open).toHaveBeenCalledWith(
    'https://www.jsonrpc.org/specification',
    '_blank',
    'noopener,noreferrer',
  )
})
it('lists source details, opens the viewer and closes the modal after selecting a document', () => {
  render(<MarkdownBody sources={seedSources}>{'See [1].'}</MarkdownBody>)
  fireEvent.click(screen.getByRole('button', { name: 'Fuentes (2)' }))
  expect(screen.getByRole('dialog')).toBeVisible()
  expect(screen.getByText(/p\. ii · párr\. 3 · Exact citations/)).toBeVisible()
  expect(screen.getByText(seedSources[0]!.excerpt!)).toBeVisible()
  expect(screen.getByRole('link', { name: 'JSON-RPC specification' })).toHaveAttribute(
    'href',
    seedSources[1]!.url,
  )
  fireEvent.click(screen.getByRole('button', { name: 'Citation guide' }))
  expect(useViewerStore.getState().target?.chunkId).toBe('demo-chunk-1')
  expect(screen.queryByRole('dialog')).not.toBeInTheDocument()
})
it('rejects unsafe source protocols', () => {
  for (const url of ['javascript:alert(1)', 'data:text/html,hi', '/relative', 'file:///etc/passwd'])
    expect(sourceUrl({ ...seedSources[1]!, url })).toBeUndefined()
})

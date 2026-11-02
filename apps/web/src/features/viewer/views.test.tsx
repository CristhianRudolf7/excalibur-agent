import { render, screen, cleanup, waitFor, act } from '@testing-library/react'
import { afterEach, beforeAll, beforeEach, describe, expect, it, vi } from 'vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import userEvent from '@testing-library/user-event'
import i18n from '@/lib/i18n'
import { Blocks } from './Blocks'
import { DocumentViewer } from './DocumentViewer'
import { scaleBox, cellPosition } from './positions'
import { SheetView } from './SheetView'
import { seedPages, seedChunk } from './seeds'
import { useViewerStore } from './store'

// Warm the lazy Markdown module outside individual assertion deadlines on shared hosts.
beforeAll(async () => {
  await import('@/components/markdown/MarkdownBody')
})

beforeEach(() => {
  void i18n.changeLanguage('en')
  HTMLElement.prototype.scrollTo = vi.fn()
  vi.spyOn(HTMLElement.prototype, 'clientWidth', 'get').mockReturnValue(600)
  vi.spyOn(HTMLElement.prototype, 'clientHeight', 'get').mockReturnValue(400)
  vi.stubGlobal(
    'ResizeObserver',
    class {
      observe() {}
      unobserve() {}
      disconnect() {}
    },
  )
})
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
  useViewerStore.getState().close()
})

describe('document views', () => {
  it.each(['docx', 'odt', 'epub', 'txt', 'md', 'html'])(
    'renders %s blocks and highlights the cited anchor',
    async (format) => {
      render(
        <Blocks page={seedPages(`demo-${format}`)[0]!} chunk={seedChunk(`demo-${format}`, 'c')} />,
      )
      expect(screen.getByRole('heading', { name: 'Document library 1' })).toBeInTheDocument()
      expect(
        screen
          .getByText('Exact citations keep answers connected to their sources.')
          .closest('[data-highlight]'),
      ).toHaveAttribute('data-highlight', 'true')
      expect(screen.getByRole('heading', { name: 'Notes' })).toBeInTheDocument()
      expect(await screen.findByRole('table', {}, { timeout: 5000 })).toBeInTheDocument()
    },
  )
  it('renders styles, blob images, nested bullet levels and inert HTML', () => {
    render(
      <Blocks
        page={{
          number: 1,
          method: 'native',
          quality: 1,
          blocks: [
            { kind: 'paragraph', text: 'Formatted', style: ['bold', 'italic'] },
            { kind: 'image', text: 'Figure', image: 'a'.repeat(64) },
            { kind: 'list_item', level: 2, text: 'Nested' },
            { kind: 'paragraph', text: '<script>alert(1)</script>' },
          ],
        }}
      />,
    )
    expect(screen.getByText('Formatted').tagName).toBe('EM')
    expect(screen.getByRole('img')).toHaveAttribute('src', `/blobs/${'a'.repeat(64)}`)
    expect(screen.getByRole('listitem')).toHaveTextContent('Nested')
    expect(document.querySelector('script')).toBeNull()
  })
  it.each(['pptx', 'odp'])('renders %s notes only when enabled', (format) => {
    const page = seedPages(`demo-${format}`)[0]!
    const { rerender } = render(<Blocks page={page} notes={false} />)
    expect(screen.queryByText('Keep the original document for reference.')).not.toBeInTheDocument()
    rerender(<Blocks page={page} notes />)
    expect(screen.getByText('Keep the original document for reference.')).toBeInTheDocument()
  })
  it.each(['xlsx', 'ods'])('renders %s read-only cells with the cited cell selected', (format) => {
    render(
      <SheetView page={seedPages(`demo-${format}`)[0]!} chunk={seedChunk(`demo-${format}`, 'c')} />,
    )
    expect(screen.getByRole('grid')).toHaveAttribute('aria-readonly', 'true')
    expect(screen.getByRole('gridcell', { name: 'B2' })).toHaveAttribute('aria-selected', 'true')
    expect(screen.getByRole('gridcell', { name: 'B2' })).toHaveTextContent('42')
  })
  it('validates spreadsheet anchors', () => {
    expect(cellPosition('Budget!AA42')).toEqual({ row: 41, column: 26 })
    expect(cellPosition('Sheet1!XFE1')).toBeUndefined()
    expect(cellPosition('Sheet1!A1048577')).toBeUndefined()
    expect(cellPosition('bad')).toBeUndefined()
  })
  it('scales PDF rectangles from a top-left origin', () => {
    expect(scaleBox({ page: 1, x0: 20, y0: 60, x1: 120, y1: 80 }, 300, 600)).toEqual({
      left: 10,
      top: 30,
      width: 50,
      height: 10,
    })
    expect(scaleBox({ page: 1, x0: 0, y0: 0, x1: 5, y1: 5 }, 300, 0).width).toBe(0)
  })
  it('opens samples, loads RPC fallback, navigates and closes', async () => {
    vi.stubGlobal(
      'fetch',
      vi.fn(async (_url, options: RequestInit) => {
        const body = JSON.parse(String(options.body)) as { id: string }
        return {
          ok: true,
          json: async () => ({
            jsonrpc: '2.0',
            id: body.id,
            error: { code: -32601, message: 'Method unavailable' },
          }),
        }
      }),
    )
    const onClose = vi.fn()
    render(
      <QueryClientProvider
        client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}
      >
        <DocumentViewer onClose={onClose} />
      </QueryClientProvider>,
    )
    await userEvent.click(screen.getByRole('button', { name: 'PPTX' }))
    expect(await screen.findByText('Sample preview')).toBeInTheDocument()
    await waitFor(() =>
      expect(screen.getByRole('heading', { name: 'Document library 1' })).toBeInTheDocument(),
    )
    const request = useViewerStore.getState().target!
    await userEvent.click(screen.getByRole('button', { name: 'Next' }))
    expect(await screen.findByRole('heading', { name: 'Document library 2' })).toBeInTheDocument()
    act(() => useViewerStore.getState().open(request))
    expect(await screen.findByRole('heading', { name: 'Document library 1' })).toBeInTheDocument()
    await userEvent.click(screen.getByRole('button', { name: 'Close document' }))
    expect(onClose).toHaveBeenCalledOnce()
    expect(useViewerStore.getState().target).toBeNull()
  })
})

it('opens the cited page and lazily requests bounded later windows', async () => {
  const requests: Array<{ method: string; params: { from?: number; to?: number } }> = []
  vi.stubGlobal(
    'fetch',
    vi.fn(async (_url, options: RequestInit) => {
      const body = JSON.parse(String(options.body)) as {
        id: string
        method: string
        params: { from: number; to: number }
      }
      requests.push(body)
      const doc = {
        ...(await import('./seeds')).seedDocument('real-doc'),
        title: 'Long document',
        format: 'pptx',
        page_count: 100,
      }
      const chunk = { ...seedChunk('real-doc', 'c42'), page: 42, anchor: 'p42', paragraph: 1 }
      const result =
        body.method === 'library.get'
          ? { document: doc }
          : body.method === 'library.chunk'
            ? { chunk }
            : {
                document: doc,
                pages: Array.from({ length: body.params.to - body.params.from + 1 }, (_, i) => ({
                  number: body.params.from + i,
                  method: 'native',
                  quality: 1,
                  blocks: [
                    {
                      kind: 'paragraph',
                      anchor: `p${body.params.from + i}`,
                      text: `Page ${body.params.from + i} content`,
                    },
                  ],
                })),
              }
      return { ok: true, json: async () => ({ jsonrpc: '2.0', id: body.id, result }) }
    }),
  )
  useViewerStore.getState().open({ documentId: 'real-doc', chunkId: 'c42' })
  render(
    <QueryClientProvider client={new QueryClient()}>
      <DocumentViewer />
    </QueryClientProvider>,
  )
  expect(await screen.findByText('Page 42 content')).toBeInTheDocument()
  expect(screen.getByText('Page 42 content').closest('[data-highlight]')).toHaveAttribute(
    'data-highlight',
    'true',
  )
  const input = screen.getByRole('spinbutton', { name: 'Slide' })
  await userEvent.clear(input)
  await userEvent.type(input, '80')
  // Controlled numeric navigation clamps partial edits; a direct change represents a page jump.
  const { fireEvent } = await import('@testing-library/react')
  fireEvent.change(input, { target: { value: '80' } })
  expect(await screen.findByText('Page 80 content')).toBeInTheDocument()
  const pageRequests = requests.filter((r) => r.method === 'library.pages')
  expect(pageRequests.some((r) => r.params.from === 41)).toBe(true)
  expect(pageRequests.some((r) => r.params.to === 80)).toBe(true)
  for (const { params } of pageRequests)
    expect(params.to! - params.from! + 1).toBeLessThanOrEqual(20)
})

it('highlights a paragraph when the citation has no anchor', () => {
  const chunk = { ...seedChunk('demo-docx', 'c'), anchor: '' }
  render(<Blocks page={seedPages('demo-docx')[0]!} chunk={chunk} />)
  expect(
    screen
      .getByText('Exact citations keep answers connected to their sources.')
      .closest('[data-highlight]'),
  ).toHaveAttribute('data-highlight', 'true')
})

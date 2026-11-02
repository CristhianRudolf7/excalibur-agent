import { afterEach, expect, it, vi } from 'vitest'
import { rpc } from '@/lib/rpc/client'
import { useWhiteboard } from '@/features/whiteboard/store'
import { addToWhiteboard, exportPNG } from './actions'
import { loadArtifact } from './api'
const svg =
  '<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 1200 600"><script>unsafe()</script><rect width="20" height="20"/></svg>'
afterEach(() => {
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  useWhiteboard.setState({ board: undefined, open: false })
})
it('adds a sanitized SVG image layer without replacing existing whiteboard content', async () => {
  const existing = {
    id: 'rect',
    type: 'rect' as const,
    x: 0,
    y: 10,
    height: 100,
    author: 'user' as const,
  }
  useWhiteboard.setState({
    board: {
      id: 'board',
      conversation_id: 'active',
      version: 1,
      title: 'Ideas',
      updated_at: '',
      elements: [existing],
    },
  })
  const call = vi.spyOn(rpc, 'call').mockResolvedValue({ version: 2 })
  await addToWhiteboard('active', svg)
  expect(call).toHaveBeenCalledWith(
    'whiteboard.apply',
    expect.objectContaining({
      board_id: 'board',
      base_version: 1,
      ops: [
        {
          op: 'add',
          elements: [
            expect.objectContaining({
              type: 'image',
              width: 640,
              height: 320,
              y: 142,
              author: 'user',
            }),
          ],
        },
      ],
    }),
  )
  const elements = useWhiteboard.getState().board!.elements
  expect(elements[0]).toEqual(existing)
  expect(elements[1]!.src).toMatch(/^data:image\/svg\+xml;charset=utf-8,/)
  expect(decodeURIComponent(elements[1]!.src!)).not.toContain('<script>')
  expect(useWhiteboard.getState().open).toBe(true)
  await expect(addToWhiteboard('another', svg)).rejects.toThrow('No conversation whiteboard')
  expect(call).toHaveBeenCalledTimes(1)
})
it('loads SVG through artifacts.get, sanitizes image bytes and validates response identity/MIME', async () => {
  const artifact = {
    id: 'one',
    title: 'One',
    kind: 'diagram' as const,
    format: 'svg' as const,
    mime: 'image/svg+xml',
    size: 10,
    created_at: '',
  }
  const call = vi
    .spyOn(rpc, 'call')
    .mockResolvedValue({ id: 'one', mime: artifact.mime, content: svg })
  const result = await loadArtifact(artifact)
  expect(call).toHaveBeenCalledWith('artifacts.get', { id: 'one' }, { signal: undefined })
  expect(result.svg).not.toContain('<script>')
  expect(result.blob.type).toBe('image/svg+xml')
  call.mockResolvedValue({ id: 'another', mime: artifact.mime, content: svg })
  await expect(loadArtifact(artifact)).rejects.toThrow('Invalid artifact content')
  call.mockResolvedValue({ id: 'one', mime: 'text/html', content: svg })
  await expect(loadArtifact(artifact)).rejects.toThrow('Invalid artifact content')
})
it.each(['png', 'pdf'] as const)('decodes base64 %s content', async (format) => {
  const mime = format === 'png' ? 'image/png' : 'application/pdf'
  vi.spyOn(rpc, 'call').mockResolvedValue({ id: 'one', mime, content: btoa('binary') })
  const result = await loadArtifact({
    id: 'one',
    title: '',
    kind: 'image',
    format,
    mime,
    created_at: '',
    size: 6,
  })
  expect(result.blob.type).toBe(mime)
  expect(result.blob.size).toBe(6)
})
it('exports SVG to PNG, limits canvas dimensions and revokes the temporary URL', async () => {
  const revoke = vi.fn()
  Object.defineProperty(URL, 'createObjectURL', {
    configurable: true,
    value: vi.fn(() => 'blob:export'),
  })
  Object.defineProperty(URL, 'revokeObjectURL', { configurable: true, value: revoke })
  vi.stubGlobal(
    'Image',
    class {
      naturalWidth = 8000
      naturalHeight = 4000
      onload?: () => void
      set src(_value: string) {
        this.onload?.()
      }
    },
  )
  const drawImage = vi.fn()
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({
    drawImage,
  } as unknown as ReturnType<HTMLCanvasElement['getContext']>)
  vi.spyOn(HTMLCanvasElement.prototype, 'toBlob').mockImplementation(function (callback) {
    callback(new Blob(['png'], { type: 'image/png' }))
  })
  const png = await exportPNG(new Blob([svg], { type: 'image/svg+xml' }))
  expect(png.type).toBe('image/png')
  expect(drawImage).toHaveBeenCalledWith(expect.anything(), 0, 0, 4096, 2048)
  expect(revoke).toHaveBeenCalledWith('blob:export')
})

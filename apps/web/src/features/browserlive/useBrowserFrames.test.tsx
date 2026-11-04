import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { connectBrowserFrames, type BrowserFrame } from './frameSocket'
import { useBrowserFrames } from './useBrowserFrames'
vi.mock('./frameSocket', () => ({ connectBrowserFrames: vi.fn(() => vi.fn()) }))
const frame = (seq: number): BrowserFrame => ({
  header: { seq, width: 800, height: 400, at: 1, tab_id: 't' },
  jpeg: new Uint8Array([255, 216, 255, 217]),
})
afterEach(() => {
  cleanup()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
  vi.mocked(connectBrowserFrames).mockClear()
})
it('decodes one image at a time, drops intermediate frames, and releases decoded images', async () => {
  const pending: ((image: ImageBitmap) => void)[] = []
  const decode = vi.fn(() => new Promise<ImageBitmap>((resolve) => pending.push(resolve)))
  vi.stubGlobal('createImageBitmap', decode)
  const drawImage = vi.fn()
  vi.spyOn(HTMLCanvasElement.prototype, 'getContext').mockReturnValue({
    drawImage,
  } as unknown as ReturnType<HTMLCanvasElement['getContext']>)
  const view = renderHook(() => useBrowserFrames('c', { id: 's', state: 'active' }))
  view.result.current.canvas.current = document.createElement('canvas')
  const receive = vi.mocked(connectBrowserFrames).mock.calls[0]![2]
  act(() => {
    receive(frame(1))
    receive(frame(2))
    receive(frame(3))
  })
  expect(decode).toHaveBeenCalledTimes(1)
  const first = { width: 8, height: 4, close: vi.fn() } as unknown as ImageBitmap
  await act(async () => pending.shift()!(first))
  expect(first.close).toHaveBeenCalledOnce()
  expect(decode).toHaveBeenCalledTimes(2)
  const last = { width: 8, height: 4, close: vi.fn() } as unknown as ImageBitmap
  await act(async () => pending.shift()!(last))
  expect(view.result.current.frame?.header.seq).toBe(3)
  expect(drawImage).toHaveBeenCalledTimes(2)
  act(() => receive(frame(2)))
  expect(decode).toHaveBeenCalledTimes(2)
  act(() => receive(frame(4)))
  view.unmount()
  await act(async () => pending.shift()!(last))
  expect(drawImage).toHaveBeenCalledTimes(2)
  expect(last.close).toHaveBeenCalledTimes(2)
})
it('uses fallback images, revokes old URLs, stops on end and releases the retained image on unmount', async () => {
  vi.stubGlobal('createImageBitmap', undefined)
  const create = vi.fn().mockReturnValueOnce('blob:one').mockReturnValueOnce('blob:two')
  const revoke = vi.fn()
  vi.stubGlobal('URL', Object.assign(URL, { createObjectURL: create, revokeObjectURL: revoke }))
  const close = vi.fn()
  vi.mocked(connectBrowserFrames).mockReturnValueOnce(close)
  const view = renderHook(({ state }) => useBrowserFrames('c', { id: 's', state }), {
    initialProps: { state: 'active' },
  })
  const receive = vi.mocked(connectBrowserFrames).mock.calls[0]![2]
  await act(async () => receive(frame(1)))
  expect(view.result.current.frame?.url).toBe('blob:one')
  await act(async () => receive(frame(2)))
  expect(revoke).toHaveBeenCalledWith('blob:one')
  view.rerender({ state: 'ended' })
  expect(close).toHaveBeenCalledOnce()
  expect(view.result.current.frame?.url).toBe('blob:two')
  view.unmount()
  expect(revoke).toHaveBeenCalledWith('blob:two')
})

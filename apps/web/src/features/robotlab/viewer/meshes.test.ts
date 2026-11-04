import { afterEach, expect, it, vi } from 'vitest'
import { RpcError, TransportError } from '@/lib/rpc/client'
const call = vi.hoisted(() => vi.fn())
vi.mock('./rpc', () => ({ viewerCall: call }))
import { decodeMesh, loadMesh } from './meshes'
afterEach(() => {
  vi.useRealTimers()
  call.mockReset()
})

function encode(values: number[], floats: boolean) {
  const buffer = new ArrayBuffer(values.length * 4)
  const view = new DataView(buffer)
  values.forEach((value, index) =>
    floats ? view.setFloat32(index * 4, value, true) : view.setUint32(index * 4, value, true),
  )
  return btoa(String.fromCharCode(...new Uint8Array(buffer)))
}
const blob = {
  key: 'triangle',
  vertices: 3,
  faces: 1,
  positions: encode([0, 0, 0, 1, 0, 0, 0, 1, 0], true),
  indices: encode([0, 1, 2], false),
}
it('decodes little-endian data and rejects malformed dimensions and indices', () => {
  expect(Array.from(decodeMesh(blob).indices)).toEqual([0, 1, 2])
  expect(() => decodeMesh({ ...blob, vertices: 2 })).toThrow('dimensions')
  expect(() => decodeMesh({ ...blob, indices: encode([0, 1, 3], false) })).toThrow('index')
})
it('deduplicates concurrent blob RPCs, computes normals and gives viewers disposable geometry', async () => {
  call.mockResolvedValue(blob)
  const [a, b] = await Promise.all([loadMesh('triangle'), loadMesh('triangle')])
  expect(call).toHaveBeenCalledTimes(1)
  expect(call).toHaveBeenCalledWith(
    'robotlab.mesh.get',
    { key: 'triangle' },
    { signal: expect.any(AbortSignal) },
  )
  expect(a).not.toBe(b)
  expect(a.getAttribute('normal').getZ(0)).toBeCloseTo(1)
  a.dispose()
  expect(b.getAttribute('position').count).toBe(3)
  b.dispose()
})
it('shares transient retries between viewers and recovers without another load call', async () => {
  vi.useFakeTimers()
  call
    .mockRejectedValueOnce(new RpcError(-32005, 'an action is already running'))
    .mockRejectedValueOnce(new TransportError('offline', 'network'))
    .mockResolvedValueOnce(blob)
  const a = loadMesh('transient')
  const b = loadMesh('transient')
  await vi.advanceTimersByTimeAsync(999)
  expect(call).toHaveBeenCalledTimes(1)
  await vi.advanceTimersByTimeAsync(1)
  expect(call).toHaveBeenCalledTimes(2)
  await vi.advanceTimersByTimeAsync(2000)
  const [first, second] = await Promise.all([a, b])
  expect(call).toHaveBeenCalledTimes(3)
  expect(first).not.toBe(second)
  first.dispose()
  second.dispose()
})
it('cancels pending mesh retries only after all viewers leave and permits a fresh load', async () => {
  vi.useFakeTimers()
  call.mockRejectedValue(new RpcError(-32020, 'runtime restarting'))
  const first = new AbortController()
  const second = new AbortController()
  const a = loadMesh('cancel', first.signal)
  const b = loadMesh('cancel', second.signal)
  const rejectedA = expect(a).rejects.toMatchObject({ kind: 'aborted' })
  const rejectedB = expect(b).rejects.toMatchObject({ kind: 'aborted' })
  await vi.advanceTimersByTimeAsync(0)
  const signal = call.mock.calls[0]![2].signal as AbortSignal
  first.abort()
  await rejectedA
  expect(signal.aborted).toBe(false)
  await vi.advanceTimersByTimeAsync(1000)
  expect(call).toHaveBeenCalledTimes(2)
  second.abort()
  await rejectedB
  expect(signal.aborted).toBe(true)
  await vi.advanceTimersByTimeAsync(60_000)
  expect(call).toHaveBeenCalledTimes(2)
  call.mockResolvedValueOnce(blob)
  const geometry = await loadMesh('cancel')
  expect(geometry.getAttribute('position').count).toBe(3)
  geometry.dispose()
})
it('does not retry missing meshes or malformed mesh data', async () => {
  call.mockRejectedValueOnce(new RpcError(-32001, 'mesh not found'))
  await expect(loadMesh('missing')).rejects.toThrow('mesh not found')
  expect(call).toHaveBeenCalledTimes(1)
  call.mockResolvedValueOnce({ ...blob, vertices: 2 })
  await expect(loadMesh('malformed')).rejects.toThrow('dimensions')
  expect(call).toHaveBeenCalledTimes(2)
})
it('allows a failed cached request to be retried', async () => {
  call.mockRejectedValueOnce(new Error('offline')).mockResolvedValueOnce(blob)
  await expect(loadMesh('retry')).rejects.toThrow('offline')
  const geometry = await loadMesh('retry')
  expect(geometry.getAttribute('position').count).toBe(3)
  geometry.dispose()
})

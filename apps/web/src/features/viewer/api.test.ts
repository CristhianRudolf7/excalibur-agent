import { describe, it, expect, vi } from 'vitest'
import { RpcError, TransportError } from '@/lib/rpc/client'
import { withSeed, pageRange } from './api'
import { seedFormats, seedPages, seedDocument, seedChunk, seedPDF } from './seeds'

describe('library viewer API', () => {
  it('uses real data when available', async () => {
    expect(
      await withSeed(
        async () => 'real',
        () => 'seed',
        false,
      ),
    ).toEqual({ value: 'real', demo: false })
  })
  it('falls back only for unavailable methods or disconnected explicit samples', async () => {
    const seed = vi.fn(() => 'sample')
    expect(
      (
        await withSeed(
          async () => {
            throw new RpcError(-32601, 'unavailable')
          },
          seed,
          false,
        )
      ).demo,
    ).toBe(true)
    expect(
      (
        await withSeed(
          async () => {
            throw new TransportError('offline', 'network')
          },
          seed,
          true,
        )
      ).value,
    ).toBe('sample')
    for (const error of [
      new RpcError(-32001, 'denied'),
      new TransportError('offline', 'network'),
      new TransportError('abort', 'aborted'),
    ]) {
      await expect(
        withSeed(
          async () => {
            throw error
          },
          seed,
          false,
        ),
      ).rejects.toBe(error)
    }
    await expect(
      withSeed(
        async () => {
          throw new TransportError('abort', 'aborted')
        },
        seed,
        true,
      ),
    ).rejects.toThrow('abort')
  })
  it('bounds all page windows to 20 and reaches later pages', () => {
    for (const page of [1, 2, 20, 22, 50, 99, 100]) {
      const { from, to } = pageRange(page, 100)
      expect(from).toBeGreaterThanOrEqual(1)
      expect(to - from + 1).toBeLessThanOrEqual(20)
      expect(page).toBeGreaterThanOrEqual(from)
      expect(page).toBeLessThanOrEqual(to)
    }
  })
  it.each(seedFormats)('has a complete %s sample', (format) => {
    const id = `demo-${format}`
    expect(seedDocument(id).format).toBe(format)
    expect(seedPages(id)).toHaveLength(seedDocument(id).page_count)
    expect(seedChunk(id, 'citation').document_id).toBe(id)
  })
  it('generates a small PDF with a valid xref start', () => {
    const text = new TextDecoder().decode(seedPDF())
    const offset = Number(/startxref\n(\d+)/.exec(text)?.[1])
    expect(text.slice(offset, offset + 4)).toBe('xref')
    expect(seedPDF().byteLength).toBeLessThan(2000)
  })
})

it('disables previews only for explicit off values', async () => {
  const { isViewerEnabled } = await import('./switches')
  expect(isViewerEnabled()).toBe(true)
  expect(isViewerEnabled('1')).toBe(true)
  expect(isViewerEnabled('true')).toBe(true)
  expect(isViewerEnabled('0')).toBe(false)
  expect(isViewerEnabled('false')).toBe(false)
})

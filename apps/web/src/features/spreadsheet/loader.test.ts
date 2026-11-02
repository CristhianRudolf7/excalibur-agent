import { expect, it, vi } from 'vitest'
import { mapSheets, loadWorkbook } from './loader'
import { useWorkbook } from './store'
import { rpc } from '@/lib/rpc/client'
import { seedDocument } from '../viewer/seeds'

it('maps names, numeric values, text and formulas from library cell blocks', () => {
  const sheets = mapSheets([
    {
      number: 1,
      title: 'Revenue',
      method: 'native',
      quality: 1,
      blocks: [
        { kind: 'cell', anchor: 'Revenue!A1', text: 'Product' },
        { kind: 'cell', anchor: 'Revenue!B2', text: '42.5' },
        { kind: 'cell', anchor: 'Revenue!AA4', text: '85', style: ['formula:=B2*2'] },
        { kind: 'paragraph', text: 'ignored' },
        { kind: 'cell', anchor: 'invalid', text: 'ignored' },
      ],
    },
  ])
  expect(sheets[0]).toEqual({
    id: 'loaded-1',
    title: 'Revenue',
    cells: { A1: 'Product', B2: 42.5, AA4: '=B2*2' },
  })
  useWorkbook.getState().replaceSheets(sheets)
  expect(useWorkbook.getState().active).toBe('loaded-1')
  expect(useWorkbook.getState().past).toEqual([])
  expect(useWorkbook.getState().sheets[0]?.store.getState().cells.AA4).toBe('=B2*2')
})
it('loads all sheets in bounded library page windows', async () => {
  const call = vi
    .spyOn(rpc, 'call')
    .mockResolvedValueOnce({ document: { ...seedDocument('demo-xlsx'), page_count: 21 } })
    .mockResolvedValueOnce({
      pages: Array.from({ length: 20 }, (_, i) => ({
        number: i + 1,
        blocks: [],
        method: 'native',
        quality: 1,
      })),
    })
    .mockResolvedValueOnce({ pages: [{ number: 21, blocks: [], method: 'native', quality: 1 }] })
  expect(await loadWorkbook('sheet-id')).toHaveLength(21)
  expect(call).toHaveBeenLastCalledWith('library.pages', {
    id: 'sheet-id',
    version: 1,
    from: 21,
    to: 21,
  })
  vi.restoreAllMocks()
})

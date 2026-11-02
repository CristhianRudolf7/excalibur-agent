import { it, expect } from 'vitest'
import { useWorkbook } from './store'
it('isolates sheets and undoes/redoes cell edits and formats', () => {
  useWorkbook.getState().setCells({ C2: 150 })
  expect(useWorkbook.getState().sheets[0]!.store.getState().cells.C2).toBe(150)
  useWorkbook.getState().undo()
  expect(useWorkbook.getState().sheets[0]!.store.getState().cells.C2).toBe(120)
  useWorkbook.getState().redo()
  expect(useWorkbook.getState().sheets[0]!.store.getState().cells.C2).toBe(150)
  useWorkbook.getState().selectSheet('forecast')
  useWorkbook.getState().setCells({ C2: 3 })
  expect(useWorkbook.getState().sheets[0]!.store.getState().cells.C2).toBe(150)
  useWorkbook.getState().setFormats(['C2'], { kind: 'percent', decimals: 1 })
  expect(useWorkbook.getState().sheets[1]!.store.getState().formats.C2?.kind).toBe('percent')
  useWorkbook.getState().undo()
  expect(useWorkbook.getState().sheets[1]!.store.getState().formats.C2).toBeUndefined()
})

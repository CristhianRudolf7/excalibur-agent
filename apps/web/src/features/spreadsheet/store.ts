import { create, createStore, type StoreApi } from 'zustand'
import { getBudgetData } from './mock-data'
import type { Cells } from './formulas'

export interface CellFormat {
  kind: 'number' | 'currency' | 'percent'
  decimals: number
}
export interface SheetData {
  cells: Cells
  formats: Record<string, CellFormat>
}
export interface Sheet {
  id: string
  title: string
  store: StoreApi<SheetData>
}
interface Snapshot {
  sheets: { id: string; title: string; data: SheetData }[]
}
export interface LoadedSheet {
  id: string
  title: string
  cells: Cells
}
interface Workbook {
  /** Name of the workspace file currently loaded, if any. */
  source?: string
  replaceSheets: (sheets: LoadedSheet[], source?: string) => void
  sheets: Sheet[]
  active: string
  past: Snapshot[]
  future: Snapshot[]
  setCells: (cells: Cells) => void
  setFormats: (addresses: string[], format: CellFormat) => void
  selectSheet: (id: string) => void
  addSheet: () => void
  undo: () => void
  redo: () => void
}
const defaultFormats: Record<string, CellFormat> = {}
for (let row = 2; row <= 8; row++)
  for (const col of ['D', 'F', 'G', 'H', 'E'])
    defaultFormats[`${col}${row}`] = {
      kind: col === 'E' ? 'percent' : 'currency',
      decimals: col === 'E' ? 0 : 2,
    }

function createSheet(id: string, title = id, data: SheetData = { cells: {}, formats: {} }): Sheet {
  return { id, title, store: createStore<SheetData>(() => data) }
}
function snapshot(sheets: Sheet[]): Snapshot {
  return { sheets: sheets.map(({ id, title, store }) => ({ id, title, data: store.getState() })) }
}
function restore(saved: Snapshot): Sheet[] {
  return saved.sheets.map(({ id, title, data }) => createSheet(id, title, data))
}
export const useWorkbook = create<Workbook>((set) => ({
  sheets: [
    createSheet('summary', 'summary', { cells: getBudgetData(), formats: defaultFormats }),
    createSheet('forecast'),
    createSheet('costs'),
  ],
  replaceSheets: (sheets, source) => {
    if (!sheets.length) throw new Error('Workbook has no sheets')
    set({
      source,
      sheets: sheets.map(({ id, title, cells }) => createSheet(id, title, { cells, formats: {} })),
      active: sheets[0]!.id,
      past: [],
      future: [],
    })
  },
  active: 'summary',
  past: [],
  future: [],
  setCells: (cells) =>
    set((s) => {
      const previous = snapshot(s.sheets)
      const sheet = s.sheets.find((sheet) => sheet.id === s.active)!
      sheet.store.setState((data) => ({ cells: { ...data.cells, ...cells } }))
      return { past: [...s.past.slice(-99), previous], future: [] }
    }),
  setFormats: (addresses, format) =>
    set((s) => {
      const previous = snapshot(s.sheets)
      const sheet = s.sheets.find((sheet) => sheet.id === s.active)!
      sheet.store.setState((data) => ({
        formats: {
          ...data.formats,
          ...Object.fromEntries(addresses.map((address) => [address, format])),
        },
      }))
      return { past: [...s.past.slice(-99), previous], future: [] }
    }),
  selectSheet: (active) => set({ active }),
  addSheet: () =>
    set((s) => {
      const id = `sheet-${s.sheets.length + 1}`
      return {
        sheets: [...s.sheets, createSheet(id)],
        active: id,
        past: [...s.past.slice(-99), snapshot(s.sheets)],
        future: [],
      }
    }),
  undo: () =>
    set((s) => {
      const last = s.past.at(-1)
      if (!last) return s
      return {
        sheets: restore(last),
        active: last.sheets.some((sheet) => sheet.id === s.active) ? s.active : last.sheets[0]!.id,
        past: s.past.slice(0, -1),
        future: [snapshot(s.sheets), ...s.future],
      }
    }),
  redo: () =>
    set((s) => {
      const next = s.future[0]
      if (!next) return s
      return {
        sheets: restore(next),
        past: [...s.past, snapshot(s.sheets)],
        future: s.future.slice(1),
      }
    }),
}))
export function formatValue(value: string | number, format?: CellFormat, language = 'es') {
  if (typeof value !== 'number') return value
  return new Intl.NumberFormat(language === 'es' ? 'es-ES' : 'en-US', {
    style:
      format?.kind === 'currency' ? 'currency' : format?.kind === 'percent' ? 'percent' : 'decimal',
    currency: 'EUR',
    useGrouping: 'always',
    minimumFractionDigits: format?.decimals ?? 0,
    maximumFractionDigits: format?.decimals ?? 2,
  }).format(value)
}

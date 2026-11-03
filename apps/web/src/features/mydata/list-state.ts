import type { Filter } from '@/lib/rpc/contract'
export type ListState = {
  query: string
  filters: Filter[]
  sort: { field: string; dir: 'asc' | 'desc' }[]
  cursors: (string | undefined)[]
}
export const initialListState: ListState = {
  query: '',
  filters: [],
  sort: [],
  cursors: [undefined],
}
export function changeList(
  state: ListState,
  patch: Partial<Omit<ListState, 'cursors'>>,
): ListState {
  return { ...state, ...patch, cursors: [undefined] }
}

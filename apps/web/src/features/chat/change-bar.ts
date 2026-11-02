export function changeBar(added: number, removed: number) {
  const total = added + removed
  if (!total) return Array<string>(5).fill('empty')
  const green =
    removed === 0 ? 5 : added === 0 ? 0 : Math.max(1, Math.min(4, Math.round((added / total) * 5)))
  return Array.from({ length: 5 }, (_, index) => (index < green ? 'add' : 'del'))
}

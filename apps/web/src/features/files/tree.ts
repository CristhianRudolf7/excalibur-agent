export function workspacePath(root: string, path: string) {
  return `${root.replace(/\/$/, '')}/${path.replace(/^\//, '')}`
}

/** Listings contain root-relative paths and /… markers for heavy directories. */
export function treeEntries(entries: string[]) {
  return entries.map((entry) => ({
    path: entry.replace(/\/(?:…)?$/, ''),
    folder: entry.endsWith('/') || entry.endsWith('/…'),
    dim: entry.endsWith('/…'),
  }))
}

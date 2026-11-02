export const sidebarWidthKey = 'sidebar-width-v1'
export const legacySidebarWidthKey = 'sidebar-width'
export const defaultSidebarWidth = 240
export const minimumSidebarWidth = 200
export const maximumSidebarWidth = 400

export function sidebarWidthLimit(viewport: number) {
  return Math.max(minimumSidebarWidth, Math.min(maximumSidebarWidth, viewport * 0.4))
}
export function clampSidebarWidth(width: number, viewport: number) {
  return Math.max(minimumSidebarWidth, Math.min(sidebarWidthLimit(viewport), width))
}
export function readSidebarWidth() {
  let width = defaultSidebarWidth
  try {
    const raw = localStorage.getItem(sidebarWidthKey) ?? localStorage.getItem(legacySidebarWidthKey)
    const candidate = Number(raw)
    if (
      raw !== null &&
      Number.isFinite(candidate) &&
      candidate >= minimumSidebarWidth &&
      candidate <= maximumSidebarWidth
    )
      width = candidate
    localStorage.setItem(sidebarWidthKey, String(width))
    localStorage.removeItem(legacySidebarWidthKey)
  } catch {
    /* Browser storage is optional. */
  }
  return width
}
export function saveSidebarWidth(width: number, viewport: number): number | null {
  if (!Number.isFinite(width) || width <= 0 || !Number.isFinite(viewport) || viewport <= 0)
    return null
  const next = clampSidebarWidth(width, viewport)
  try {
    localStorage.setItem(sidebarWidthKey, String(next))
  } catch {
    /* Keep the in-memory preference. */
  }
  return next
}

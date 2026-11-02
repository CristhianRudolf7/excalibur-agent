import type { Camera } from './engine/camera'
export interface WorldDebug {
  running: boolean
  fps: number
  mode: 'campus' | 'room' | 'chat'
  ready: boolean
  camera?: Camera
  elapsed?: number
  advance?: (milliseconds: number) => void
  settle?: () => void
}
declare global {
  interface Window {
    __excaliburWorld?: WorldDebug
  }
}
export function worldDebugEnabled() {
  return (
    import.meta.env.MODE !== 'production' ||
    new URLSearchParams(window.location.search).get('worldDebug') === '1'
  )
}
export function manualWorldClock() {
  return (
    worldDebugEnabled() &&
    new URLSearchParams(window.location.search).get('worldClock') === 'manual'
  )
}
export function publishWorldDebug(state: WorldDebug) {
  if (worldDebugEnabled()) window.__excaliburWorld = state
}

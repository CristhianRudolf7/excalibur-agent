import { snapZoom } from './engine/camera'
export const speedOptions = [0.5, 1, 2, 4] as const
/** Cycles the public movement presets; non-preset debug speeds rejoin at the slowest preset. */
export function nextSpeed(speed: number) {
  const index = speedOptions.findIndex((value) => value === speed)
  return speedOptions[(index + 1) % speedOptions.length]!
}
export function normalizeSpeed(speed: number) {
  return Number.isFinite(speed) ? Math.max(0.25, Math.min(16, speed)) : 1
}
export function readWorldPreference(key: string, fallback: number) {
  try {
    const value = localStorage.getItem(key)
    return value === null || !Number.isFinite(Number(value)) ? fallback : Number(value)
  } catch {
    return fallback
  }
}
export function saveWorldPreference(key: string, value: number) {
  try {
    localStorage.setItem(key, String(value))
  } catch {
    /* Preferences are optional. */
  }
}
export function initialWorldSettings() {
  return {
    speed: normalizeSpeed(readWorldPreference('world-speed', 1)),
    zoom: snapZoom(readWorldPreference('world-zoom', 0.75)),
  }
}

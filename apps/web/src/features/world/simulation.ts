import type { WorldEvent } from './events'
export interface ScriptedEvent {
  offset: number
  event: Omit<WorldEvent, 'timestamp'>
}
export interface EventProducer {
  advance: (milliseconds: number) => WorldEvent[]
  elapsed: number
}
/** A deterministic clock-driven producer. No renderer, component or store depends on its provenance. */
export function createSimulation(
  script: readonly ScriptedEvent[],
  duration: number,
  epoch: number,
  elapsed = 0,
): EventProducer {
  return {
    elapsed,
    advance(milliseconds) {
      if (!Number.isFinite(milliseconds) || milliseconds <= 0 || duration <= 0) return []
      const from = this.elapsed,
        to = from + milliseconds,
        events: WorldEvent[] = []
      for (let cycle = Math.floor(from / duration); cycle <= Math.floor(to / duration); cycle++) {
        for (const entry of script) {
          const at = cycle * duration + entry.offset
          if (at > from && at <= to)
            events.push({ ...entry.event, id: `${cycle}:${entry.event.id}`, timestamp: epoch + at })
        }
      }
      this.elapsed = to
      return events
    },
  }
}
export function worldSpeed(search: string, development: boolean) {
  if (!development) return 1
  const speed = Number(new URLSearchParams(search).get('worldSpeed') ?? 1)
  return Number.isFinite(speed) ? Math.max(0.25, Math.min(16, speed)) : 1
}

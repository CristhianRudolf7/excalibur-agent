import type { RunState } from './RunState'
import type { JSONSchema, MetricPoint } from './types'
/** Stable run/step identity merges history with events, including corrected samples. */
export function mergeMetrics(previous: MetricPoint[], incoming: MetricPoint[], limit = 10000) {
  const points = new Map(previous.map((point) => [`${point.run}:${point.step}`, point]))
  for (const point of incoming)
    points.set(`${point.run}:${point.step}`, {
      ...points.get(`${point.run}:${point.step}`),
      ...point,
    })
  return [...points.values()]
    .sort((a, b) => a.step - b.step || a.run.localeCompare(b.run))
    .slice(-limit)
}
export function parameterFields(schema: JSONSchema) {
  return Object.entries(schema.properties ?? {}).map(([name, field]) => ({
    ...field,
    name,
    required: schema.required?.includes(name) ?? false,
  }))
}
/** Coalesce changes, send the trailing neutral input, and never send unchanged values. */
export function inputThrottle<T>(send: (value: T) => void, interval = 1000 / 30) {
  let last = -Infinity,
    signature = '',
    pending: T | undefined,
    timer: ReturnType<typeof setTimeout> | undefined
  const flush = () => {
    timer = undefined
    if (pending === undefined) return
    const value = pending
    pending = undefined
    const next = JSON.stringify(value)
    if (next === signature) return
    last = performance.now()
    signature = next
    send(value)
  }
  return {
    push(value: T) {
      pending = value
      if (timer !== undefined) return
      const wait = interval - (performance.now() - last)
      if (wait <= 0) flush()
      else timer = setTimeout(flush, Math.ceil(wait))
    },
    cancel() {
      clearTimeout(timer)
      pending = undefined
      timer = undefined
    },
  }
}

export function experimentState(status: string): RunState {
  return status === 'failed'
    ? 'error'
    : status === 'running' || status === 'done'
      ? status
      : 'stopped'
}

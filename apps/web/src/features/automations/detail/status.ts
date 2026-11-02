import type { AutomationRun, AutomationStepRunStatus } from '@/lib/rpc/contract'

export function stepStatus(
  run: AutomationRun | undefined,
  id: string,
): AutomationStepRunStatus | undefined {
  if (!run) return undefined
  return run.steps.find((step) => step.step_id === id)?.status ?? 'pending'
}
export function edgeTraversed(run: AutomationRun | undefined, from: string, to: string) {
  if (!run) return false
  const source = from === '__schedule' ? 'succeeded' : stepStatus(run, from)
  const target = stepStatus(run, to)
  return source === 'succeeded' && target !== 'pending' && target !== 'skipped'
}

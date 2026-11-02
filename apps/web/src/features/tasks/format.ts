import type { TaskSummary, TaskStatus } from '@/lib/rpc/contract'
export const taskStatuses: TaskStatus[] = [
  'pending',
  'in_progress',
  'blocked',
  'done',
  'failed',
  'cancelled',
]
export function criteriaProgress(task: Pick<TaskSummary, 'criteria_met' | 'criteria_total'>) {
  return task.criteria_total > 0
    ? Math.min(100, Math.max(0, (task.criteria_met / task.criteria_total) * 100))
    : 0
}

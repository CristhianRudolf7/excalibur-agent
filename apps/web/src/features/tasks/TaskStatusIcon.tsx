import { Check, Minus, X } from 'lucide-react'
import type { TaskStatus } from '@/lib/rpc/contract'

export function TaskStatusIcon({ status }: { status: TaskStatus }) {
  return (
    <span className={`work-status-icon work-status-${status}`} aria-hidden="true">
      {status === 'done' ? (
        <Check />
      ) : status === 'blocked' ? (
        <Minus />
      ) : status === 'failed' || status === 'cancelled' ? (
        <X />
      ) : null}
    </span>
  )
}

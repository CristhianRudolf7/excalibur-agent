import type { ClassroomContext } from '@/lib/rpc/classroom'
import type { FileChange } from '@/lib/rpc/contract'
import type { SourceRef } from '@/features/library/types'
export interface Message {
  id: string
  context?: ClassroomContext
  clientMsgId?: string
  attachments?: string[]
  delivery?: 'sending' | 'failed'
  error?: string
  relayedBy?: string
  sender: 'user' | 'agent'
  text: string
  time: string
  literal?: boolean
  interaction?: { kind: 'question' | 'plan'; id: string; resolved?: boolean }
  sources?: SourceRef[]
  changes?: FileChange[]
}

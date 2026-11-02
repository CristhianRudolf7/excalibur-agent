import type { FileChange } from '@/lib/rpc/contract'
import type { SourceRef } from '@/features/library/types'
export interface Message {
  id: string
  clientMsgId?: string
  attachments?: string[]
  delivery?: 'sending' | 'failed'
  error?: string
  sender: 'user' | 'agent'
  text: string
  time: string
  literal?: boolean
  interaction?: { kind: 'question' | 'plan'; id: string; resolved?: boolean }
  sources?: SourceRef[]
  changes?: FileChange[]
}

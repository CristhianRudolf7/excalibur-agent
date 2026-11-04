export interface BrowserSessionEvent {
  conversation_id: string
  run_id: string
  session_id: string
  state: 'active' | 'ended'
  url?: string
  title?: string
}
export interface BrowserActivityEvent {
  conversation_id: string
  run_id: string
  session_id: string
  tab_id?: string
  seq: number
  phase: 'start' | 'done' | 'failed'
  kind: string
  target?: string
  url?: string
  title?: string
  box?: { x: number; y: number; width: number; height: number }
}
export interface BrowserLiveState {
  session?: BrowserSessionEvent
  activity?: BrowserActivityEvent
}

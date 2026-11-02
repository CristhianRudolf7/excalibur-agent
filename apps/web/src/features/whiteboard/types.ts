export type ElementType =
  | 'rect'
  | 'ellipse'
  | 'diamond'
  | 'line'
  | 'arrow'
  | 'text'
  | 'freedraw'
  | 'image'
  | 'frame'
  | 'math'
  | 'mermaid'
  | 'code'
  | 'sticky'
export interface Element {
  id: string
  type: ElementType
  x: number
  y: number
  width?: number
  height?: number
  points?: readonly (readonly [number, number])[]
  text?: string
  latex?: string
  mermaid?: string
  code?: string
  language?: string
  src?: string
  start_binding?: { element_id: string; focus?: number; gap?: number } | null
  end_binding?: { element_id: string; focus?: number; gap?: number } | null
  style?: {
    stroke?: string
    fill?: string
    stroke_width?: number
    stroke_style?: 'solid' | 'dashed' | 'dotted'
    roughness?: number
    font_size?: number
    font_family?: string
    text_align?: 'left' | 'center' | 'right'
    opacity?: number
  }
  group_id?: string
  frame_id?: string
  z?: number
  locked?: boolean
  author: 'agent' | 'user'
}
export interface BoardSummary {
  id: string
  conversation_id: string
  title: string
  version: number
  updated_at: string
}
export interface Board extends BoardSummary {
  elements: Element[]
  viewport?: { x: number; y: number; zoom: number }
}
export interface Op {
  op: 'add' | 'update' | 'remove' | 'clear' | 'set_scene'
  elements?: (Partial<Element> & { id: string })[]
  ids?: string[]
}
export interface BoardEvent {
  conversation_id: string
  board_id?: string
  board?: Board
  layout?: 'panel' | 'fullscreen'
  version?: number
  ops?: Op[]
  animate?: boolean
  step_ms?: number
  author?: 'agent' | 'user'
  ids?: string[]
  rect?: { x: number; y: number; width: number; height: number }
  zoom?: number
  style?: 'laser' | 'pulse' | 'box'
  duration_ms?: number
  request_id?: string
}
export interface WhiteboardMethods {
  'whiteboard.list': { params: { conversation_id?: string }; result: { boards: BoardSummary[] } }
  'whiteboard.get': { params: { board_id: string }; result: Board }
  'whiteboard.apply': {
    params: { board_id: string; base_version: number; ops: Op[]; client_op_id: string }
    result: { version: number }
  }
  'whiteboard.snapshot': {
    params: { board_id: string; image_blob: string; request_id?: string }
    result: { ok: boolean }
  }
  'whiteboard.close': { params: { board_id: string }; result: unknown }
}

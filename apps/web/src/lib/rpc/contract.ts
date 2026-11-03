import type { AgentMethods, AgentEvents } from '@/features/agents/types'
import type { CredentialMethods, CredentialEvents } from './credentials'
import type { ClassroomMethods, ClassroomContext } from './classroom'
import type { Artifact, ArtifactContent } from '@/features/artifacts/types'
import type { WhiteboardMethods } from '@/features/whiteboard/types'
import type { RobotState } from '@/features/robot/types'
/** Wire types mirror internal/api and packages/contracts; only connected methods belong here. */
export interface Principal {
  id: string
  name: string
  timezone: string
  timezone_source: 'default' | 'auto' | 'manual'
  language: string
}
export const clientMethods = [
  'credentials.pending',
  'credentials.submit',
  'credentials.cancel',
  'vault.list_metadata',
  'vault.unlock',
  'vault.lock',
  'vault.delete',
  'activity.list',
  'tasks.query',
  'tasks.get',
  'reminders.list',
  'reminders.create',
  'reminders.update',
  'reminders.delete',
  'automations.list',
  'automations.get',
  'automations.runs',
  'automations.run',
  'automations.pause',
  'automations.resume',
  'automations.delete',
  'conversation.send',
  'conversation.history',
  'conversation.subscribe',
  'principal.get',
  'principal.update',
  'voice.status',
  'voice.transcribe',
  'voice.speak',
  'files.open',
  'library.ingest',
  'library.list',
  'library.get',
  'library.pages',
  'library.chunk',
  'library.delete',
  'library.cancel',
] as const satisfies readonly (keyof RpcMethods)[]

export interface Box {
  page: number
  x0: number
  y0: number
  x1: number
  y1: number
}
export interface DocBlock {
  kind: string
  level?: number
  text: string
  anchor?: string
  boxes?: Box[]
  style?: string[]
  image?: string
}
export interface DocPage {
  number: number
  label?: string
  title?: string
  width?: number
  height?: number
  method: string
  quality: number
  blocks: DocBlock[]
  warnings?: string[]
}
export interface DocumentInfo {
  id: string
  title: string
  source_name: string
  mime: string
  format: string
  size: number
  blob_digest: string
  scope_id: string
  version: number
  status: 'pending' | 'processing' | 'ready' | 'partial' | 'failed' | 'canceled'
  progress_pages: number
  total_pages: number
  page_count: number
  paginated: boolean
  uploaded_at: string
  updated_at: string
  error: string
  failure_stage: string
  warnings: string[]
  summary: string
}
export interface ChunkDetail {
  chunk_id: string
  document_id: string
  version: number
  page: number
  page_label: string
  paragraph: number
  section_path: string
  anchor: string
  text: string
  boxes: Box[]
}
export interface SourceRef {
  n?: number
  kind: 'document' | 'web'
  document_id?: string
  chunk_id?: string
  title: string
  page?: number
  page_label?: string
  paragraph?: number
  section_path?: string
  excerpt?: string
  boxes?: Box[]
  mime?: string
  blob_digest?: string
  verbatim?: string
  domain?: string
  read_at?: string
  tool?: string
  snippet?: string
  url?: string
}
export interface RpcEvents extends CredentialEvents, AgentEvents {
  'library.document_updated': DocumentInfo
}
export interface FileHunk {
  old_start: number
  old_lines: number
  new_start: number
  new_lines: number
  lines: string[]
}
export interface FileChange {
  path: string
  status: 'added' | 'modified' | 'deleted'
  added: number
  removed: number
  hunks: FileHunk[]
  content?: string
  truncated?: boolean
  binary?: boolean
}
export interface ConversationMessage {
  id: string
  conversation_id: string
  role: string
  text: string
  relayed_by?: string
  sources?: SourceRef[]
  changes?: FileChange[]
}
export interface ConversationEvent {
  seq: number
  id: string
  type: string
  at: string
  run_id?: string
  task_id?: string
  payload:
    | {
        agent_id?: string
        unread?: number
        last_preview?: string
        last_message_at?: string
        course_id?: string
        topic_id?: string
        quiz_id?: string
        attempt_id?: string
        final?: boolean
        feedback?: string
        session_id?: string
        turn_id?: string
        artifact?: Artifact
        artifacts?: Artifact[]
        item?: ActivityItem
        automation?: Automation
        run?: AutomationRun
        id?: string
        task?: TaskSummary
        reminder?: Reminder
        message_id?: string
        conversation_id?: string
        role?: string
        relayed_by?: string
        text?: string
        sources?: SourceRef[]
        changes?: FileChange[]
        notice_id?: string
        content?: string
        run_id?: string
        state?: string
        question_id?: string
        plan_id?: string
        summary?: string
        tool?: string
        status?: string
      }
    | DocumentInfo
}
export interface ActivityItem {
  id: string
  kind: 'subagent' | 'task' | 'automation'
  name: string
  goal?: string
  profile?: string
  state: 'queued' | 'running' | 'waiting' | 'paused' | 'completed' | 'failed' | 'cancelled'
  step?: string
  progress?: { done: number; total: number }
  parent_id?: string
  started_at: string
  updated_at: string
  finished_at?: string
  error?: string
}
export interface RpcMethods
  extends WhiteboardMethods, ClassroomMethods, AgentMethods, CredentialMethods {
  'artifacts.get': { params: { id: string }; result: ArtifactContent }
  'activity.list': { params: { conversation_id: string }; result: { items: ActivityItem[] } }

  'question.answer': { params: { question_id: string; answer: string }; result: unknown }
  'plan.approve': { params: { plan_id: string; reason?: string }; result: unknown }
  'plan.deny': { params: { plan_id: string; reason?: string }; result: unknown }
  'voice.status': {
    params: undefined
    result: {
      available: boolean
      stt_model: string
      tts_model: string
      voices: string[]
      reason?: string
    }
  }
  'voice.transcribe': {
    params: { audio_blob: string; mime: string; language?: string }
    result: { text: string; language: string; duration_ms: number }
  }
  'voice.speak': {
    params: { text: string; voice?: string; format?: 'mp3' | 'opus' | 'wav' }
    result: { audio_blob: string; mime: string; duration_ms?: number }
  }

  'conversation.send': {
    params: {
      conversation_id: string
      client_msg_id: string
      text: string
      attachments?: string[]
      context?: ClassroomContext
    }
    result: { msg_id: string }
  }
  'conversation.history': {
    params: { conversation_id: string; before?: string; limit?: number }
    result: { messages: ConversationMessage[] | null }
  }
  'conversation.subscribe': {
    params: { cursor?: string; types?: string[] }
    result: { events: ConversationEvent[] | null; cursor: string }
  }
  'principal.get': { params: undefined; result: Principal }
  'principal.update': {
    params: {
      timezone: string
      timezone_source: Principal['timezone_source']
      enable_auto?: boolean
    }
    result: Principal
  }
  'library.ingest': {
    params: { blob_digest: string; name: string; mime: string; title?: string; scope_id?: string }
    result: { document: DocumentInfo; existing: boolean }
  }
  'library.list': {
    params: {
      scope_id?: string
      query?: string
      status?: DocumentInfo['status']
      cursor?: string
      limit?: number
    }
    result: { documents: DocumentInfo[]; next_cursor: string }
  }
  'library.get': { params: { id: string }; result: { document: DocumentInfo } }
  'library.pages': {
    params: { id: string; version?: number; from: number; to: number }
    result: { document: DocumentInfo; pages: DocPage[] }
  }
  'library.chunk': {
    params: { document_id: string; chunk_id: string }
    result: { chunk: ChunkDetail }
  }
  'library.delete': { params: { id: string }; result: { deleted: boolean; notice: string } }
  'library.cancel': { params: { id: string }; result: { document: DocumentInfo } }
}

export type TaskStatus = 'pending' | 'in_progress' | 'blocked' | 'done' | 'failed' | 'cancelled'
export interface TaskSummary {
  id: string
  short_code: string
  title: string
  status: TaskStatus
  level: 'brief' | 'project' | 'initiative'
  project_id?: string
  created_at: string
  updated_at: string
  due_at?: string
  criteria_total: number
  criteria_met: number
  blocked_reason?: string
}
export interface TaskDetail extends TaskSummary {
  request: string
  requirements: { id: string; text: string }[]
  criteria: {
    id: string
    text: string
    state: 'met' | 'failed' | 'unverified' | 'n/a'
    method: string
  }[]
  evidence: { criterion_id: string; kind: string; summary: string; at: string; ok: boolean }[]
  plan: { id: string; title: string; status: string; depends_on: string[] }[]
  notes?: string
  runs: { id: string; state: string; started_at: string; ended_at?: string }[]
}
export interface Reminder {
  id: string
  text: string
  due_at: string
  timezone: string
  repeat?: string
  status: 'scheduled' | 'done' | 'snoozed' | 'cancelled'
  created_at: string
  last_fired_at?: string
}
export interface Project {
  id: string
  name: string
  alias?: string
  description?: string
  roots: string[]
  status: string
  created_at: string
  updated_at: string
}
export interface RpcMethods {
  'tasks.query': {
    params: {
      status?: TaskStatus[]
      project_id?: string
      text?: string
      limit?: number
      cursor?: string
    }
    result: { tasks: TaskSummary[]; next_cursor?: string; counts: Record<TaskStatus, number> }
  }
  'tasks.get': { params: { id: string }; result: TaskDetail }
  'reminders.list': { params: { include_done?: boolean }; result: { reminders: Reminder[] } }
  'reminders.create': {
    params: { text: string; due_at: string; timezone?: string; repeat?: string }
    result: Reminder
  }
  'reminders.update': {
    params: {
      id: string
      text?: string
      due_at?: string
      repeat?: string
      status?: Reminder['status']
      snooze_until?: string
    }
    result: Reminder
  }
  'reminders.delete': { params: { id: string }; result: { id: string; deleted: true } }
  'automations.list': { params: Record<string, never>; result: { automations: Automation[] } }
  'automations.get': {
    params: { id: string }
    result: { automation: Automation; versions: AutomationVersion[] }
  }
  'automations.runs': { params: { id: string; limit?: number }; result: { runs: AutomationRun[] } }
  'automations.run': { params: { id: string }; result: { run: AutomationRun } }
  'automations.pause': { params: { id: string }; result: Automation }
  'automations.resume': { params: { id: string }; result: Automation }
  'automations.delete': { params: { id: string }; result: { id: string; deleted: true } }
  'project.list': { params: undefined; result: { projects: Project[] | null } }
  'project.get': { params: { project_id: string }; result: Project }
  'project.register': {
    params: { name: string; roots: string[]; description?: string }
    result: Project
  }
  'files.list': {
    params: { project_id: string; path?: string; cursor?: string; limit?: number }
    result: { entries: string[] | null; next_cursor: string }
  }
}
export interface RpcEvents {
  'task.updated': { task: TaskSummary }
  'reminder.updated': { reminder: Reminder }
  'reminder.fired': { reminder: Reminder }
  'automation.updated': { automation: Automation }
  'automation.deleted': { id: string }
  'automation.run': { run: AutomationRun }
}
export type AutomationStatus = 'active' | 'paused'
export type ScheduleKind = 'once' | 'interval' | 'daily' | 'weekly' | 'monthly' | 'cron'
export interface AutomationSchedule {
  kind: ScheduleKind
  timezone: string
  at?: string
  every_minutes?: number
  time?: string
  weekdays?: number[]
  day_of_month?: number
  cron?: string
}
export type AutomationStepType = 'agent' | 'script' | 'tool' | 'condition' | 'notify'
export interface AutomationStep {
  id: string
  type: AutomationStepType
  title: string
  instruction?: string
  language?: 'python' | 'node' | 'bash'
  code?: string
  tool?: string
  args?: Record<string, unknown>
  expression?: string
  message?: string
}
export interface AutomationEdge {
  from: string
  to: string
  label?: 'yes' | 'no'
}
export type AutomationRunStatus = 'running' | 'succeeded' | 'failed' | 'cancelled'
export type AutomationStepRunStatus = 'pending' | 'running' | 'succeeded' | 'failed' | 'skipped'
export interface AutomationStepRun {
  step_id: string
  status: AutomationStepRunStatus
  started_at?: string
  finished_at?: string
  output_preview?: string
  error?: string
}
export interface AutomationRun {
  id: string
  automation_id: string
  version: number
  trigger: 'schedule' | 'manual' | 'catch_up'
  status: AutomationRunStatus
  started_at: string
  finished_at?: string
  summary?: string
  error?: string
  steps: AutomationStepRun[]
}
export interface Automation {
  id: string
  name: string
  description: string
  status: AutomationStatus
  paused_reason?: string
  version: number
  schedule: AutomationSchedule
  steps: AutomationStep[]
  edges: AutomationEdge[]
  next_run_at?: string
  last_run?: AutomationRun
  consecutive_failures: number
  created_at: string
  updated_at: string
}
export interface AutomationVersion {
  version: number
  created_at: string
  note?: string
}
export type SkillSource = 'builtin' | 'user' | 'installed' | 'project'
export interface SkillSummary {
  name: string
  group: string
  description: string
  version: string
  source: SkillSource
  state: string
  editable: boolean
  deletable: boolean
  tools: string[]
}
export interface SkillGroup {
  id: string
  name_en: string
  name_es: string
  description_en: string
  description_es: string
  count: number
}
export interface SkillDetail extends SkillSummary {
  content: string
  path_kind: SkillSource
  diagnostics: string[]
}
export interface UsageTokens {
  input_tokens: number
  cached_tokens: number
  output_tokens: number
  cost_usd: number
  tool_calls: number
}
export interface UsageSummary {
  totals: UsageTokens & { runs: number }
  by_bucket: (UsageTokens & { start: string })[]
  by_model: { model: string; input_tokens: number; output_tokens: number; cost_usd: number }[]
  by_tool: { tool: string; calls: number; failures: number }[]
  recent_runs: {
    run_id: string
    task_short_code?: string
    started_at: string
    duration_ms: number
    input_tokens: number
    output_tokens: number
    cost_usd: number
    tool_calls: number
    status: string
  }[]
}
/** Additive INT-2 client contract; backend implementation is owned by BE-2. */
export interface RpcMethods {
  'skills.groups': { params: undefined; result: { groups: SkillGroup[] } }
  'skills.list': {
    params: { group?: string; text?: string; source?: SkillSource }
    result: { skills: SkillSummary[] }
  }
  'skills.get': { params: { name: string }; result: SkillDetail }
  'skills.update': { params: { name: string; content: string }; result: SkillSummary }
  'skills.set_state': {
    params: { name: string; state: 'enabled' | 'disabled' }
    result: SkillSummary
  }
  'skills.delete': { params: { name: string }; result: { name: string; deleted: true } }
  'usage.summary': {
    params: { since?: string; until?: string; bucket?: 'hour' | 'day' }
    result: UsageSummary
  }
}

/** Workspace file routing; document and sheet IDs use the library APIs. */
export interface WorkspaceFile {
  path: string
  mime: string
  kind: 'text' | 'document' | 'sheet' | 'image' | 'binary'
  content?: string
  document_id?: string
  blob_url?: string
  size: number
}
export interface RpcMethods {
  'files.open': { params: { path: string; project_id?: string }; result: WorkspaceFile }
}

/** Camera pixels to robot base XY; mirrors internal/api/robot.go. */
export interface RobotCalibrationPoint {
  pixel: [number, number]
  base: [number, number]
}
export interface RobotImageSize {
  width: number
  height: number
}
export interface RobotCalibrationStatus {
  calibrated: boolean
  points: RobotCalibrationPoint[]
  table_z: number
  image: RobotImageSize
  reprojection_error_px: number
  error?: string
}
export interface RpcEvents {
  'robot.state': { state: RobotState; calibration?: RobotCalibrationStatus }
}
export interface RpcMethods {
  'robot.calibration.status': { params: undefined; result: RobotCalibrationStatus }
  'robot.calibration.add_point': {
    params: { pixel: [number, number] }
    result: RobotCalibrationStatus
  }
  'robot.calibration.set_table_z': {
    params: { z: number; use_current?: false } | { use_current: true; z?: never }
    result: RobotCalibrationStatus
  }
  'robot.calibration.clear': { params: undefined; result: RobotCalibrationStatus }
  'robot.calibration.set': {
    params: { points: RobotCalibrationPoint[]; table_z: number; image: RobotImageSize }
    result: RobotCalibrationStatus
  }
}

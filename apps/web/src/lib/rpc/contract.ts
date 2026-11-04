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
export interface GoogleStatus {
  connected: boolean
  configured: boolean
  email?: string
  scopes?: string[]
  expires_at?: string
  reason?: string
  warning?: string
  missing_scopes?: string[]
}
export const clientMethods = [
  // BEGIN GENERATED ROBOT LAB METHODS
  'robotlab.status',
  'robotlab.install',
  'robotlab.robots.list',
  'robotlab.env.list',
  'robotlab.env.get',
  'robotlab.env.delete',
  'robotlab.env.thumbnail',
  'robotlab.env.scene',
  'robotlab.env.preview',
  'robotlab.env.save',
  'robotlab.settings.get',
  'robotlab.settings.set',
  'robotlab.session.start',
  'robotlab.session.list',
  'robotlab.session.get',
  'robotlab.session.stop',
  'robotlab.session.scene',
  'robotlab.session.image',
  'robotlab.session.log',
  'robotlab.attempt.reset',
  'robotlab.attempt.frames',
  'robotlab.robot.act',
  'robotlab.robot.look',
  'robotlab.robot.state',
  'robotlab.skills.list',
  'robotlab.skills.read',
  'robotlab.skills.use',
  'robotlab.skills.write',
  'robotlab.skills.copy',
  'robotlab.log.note',
  'robotlab.mesh.get',
  'robotlab.session.resume',
  // END GENERATED ROBOT LAB METHODS
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
  'google.status',
  'google.connect.start',
  'google.connect.complete',
  'google.disconnect',
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
        collection?: string
        record_id?: string
        kind?: string
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
  'google.status': { params: Record<string, never>; result: GoogleStatus }
  'google.connect.start': { params: Record<string, never>; result: { auth_url: string } }
  'google.connect.complete': { params: { code: string; state: string }; result: GoogleStatus }
  'google.disconnect': { params: Record<string, never>; result: GoogleStatus }
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

/** Personal collections: additive shared contract, implemented by the daemon worktree. */
export type FieldType =
  | 'text'
  | 'long_text'
  | 'number'
  | 'integer'
  | 'money'
  | 'date'
  | 'datetime'
  | 'boolean'
  | 'select'
  | 'multi_select'
  | 'relation'
  | 'email'
  | 'url'
  | 'file'
export type Field = {
  id: string
  name: string
  type: FieldType
  required?: boolean
  unique?: boolean
  options?: string[]
  currency?: string
  target?: string
  min?: number
  max?: number
  description?: string
}
export type Filter = {
  field: string
  op: 'eq' | 'neq' | 'lt' | 'lte' | 'gt' | 'gte' | 'contains' | 'in' | 'is_empty' | 'not_empty'
  value?: unknown
}
export type SummaryCard = {
  id: string
  title: string
  kind: 'count' | 'sum' | 'avg' | 'min' | 'max' | 'weighted_avg'
  field?: string
  weight_field?: string
  filter?: Filter[]
  group_by?: string
  chart?: 'none' | 'bar' | 'line' | 'pie'
}
export type Collection = {
  id: string
  name: string
  icon?: string
  description?: string
  fields: Field[]
  display_field: string
  summary?: SummaryCard[]
  record_count: number
  created_at: string
  updated_at: string
  version: number
}
export type RecordRow = {
  id: string
  values: Record<string, unknown>
  created_at: string
  updated_at: string
}
export type ImportReport = {
  dry_run: boolean
  total_rows: number
  imported: number
  skipped: number
  updated: number
  mapping: { column: string; field: string | null }[]
  unknown_columns: string[]
  conflicts: {
    row: number
    column: string
    value: string
    problem:
      | 'invalid_type'
      | 'required_missing'
      | 'duplicate'
      | 'unknown_option'
      | 'out_of_range'
      | 'ambiguous_date'
      | 'relation_not_found'
    suggestion?: string
  }[]
  fixes_applied: { row: number; column: string; from: string; to: string; rule: string }[]
  pending_rows?: { row: number; values: Record<string, string>; reason: string }[]
}
export interface RpcMethods {
  'collections.list': { params: Record<string, never>; result: { collections: Collection[] } }
  'collections.get': { params: { id: string }; result: { collection: Collection } }
  'collections.create': {
    params: {
      name: string
      icon?: string
      description?: string
      fields: Field[]
      display_field?: string
      summary?: SummaryCard[]
    }
    result: { collection: Collection }
  }
  'collections.update': {
    params: {
      id: string
      name?: string
      icon?: string
      description?: string
      summary?: SummaryCard[]
      display_field?: string
    }
    result: { collection: Collection }
  }
  'collections.fields': {
    params: {
      id: string
      add?: Field[]
      rename?: { field: string; name: string }[]
      remove?: string[]
      change_type?: { field: string; type: FieldType; options?: string[] }[]
      dry_run?: boolean
    }
    result: {
      collection: Collection
      report?: {
        field: string
        converted: number
        failed: { record_id: string; value: unknown }[]
      }[]
    }
  }
  'collections.delete': { params: { id: string }; result: { ok: boolean } }
  'records.list': {
    params: {
      collection: string
      query?: string
      filters?: Filter[]
      sort?: { field: string; dir: 'asc' | 'desc' }[]
      limit?: number
      cursor?: string
    }
    result: { records: RecordRow[]; total: number; next_cursor?: string }
  }
  'records.get': {
    params: { collection: string; id: string }
    result: {
      record: RecordRow
      related: { collection: string; field: string; records: RecordRow[] }[]
    }
  }
  'records.create': {
    params: { collection: string; values: Record<string, unknown> }
    result: { record: RecordRow }
  }
  'records.update': {
    params: { collection: string; id: string; values: Record<string, unknown> }
    result: { record: RecordRow }
  }
  'records.delete': { params: { collection: string; id: string }; result: { ok: boolean } }
  'collections.summary': {
    params: { id: string }
    result: {
      cards: {
        id: string
        title: string
        value: number
        unit?: string
        series?: { label: string; value: number }[]
      }[]
    }
  }
  'collections.import': {
    params: {
      collection?: string
      blob_digest: string
      sheet?: string
      mapping?: { column: string; field: string | null }[]
      create_schema?: boolean
      upsert_key?: string
      fixes?: 'auto' | 'none'
      dry_run?: boolean
      overrides?: { row: number; column: string; value: string }[]
      skip_rows?: number[]
    }
    result: { collection?: Collection; report: ImportReport; proposed_fields?: Field[] }
  }
  'collections.export': {
    params: { id: string; format: 'xlsx' | 'csv'; filters?: Filter[] }
    result: { blob_digest: string; name: string; mime: string }
  }
}
export interface RpcEvents {
  'collections.changed': { collection: string; record_id?: string; kind: string }
}

// BEGIN GENERATED ROBOT LAB — run python3 scripts/gen-robotlab-contracts.py
export type RobotLabTarget = { point: number[] } | { object: string } | { robot_start: true }
export type RobotLabGoal =
  | { type: 'object_near'; object: string; target: RobotLabTarget; radius: number }
  | { type: 'robot_near'; target: RobotLabTarget; radius: number }
  | { type: 'object_held'; object: string }
  | { type: 'object_on'; object: string; target: { object: string }; stable_s?: number }
  | { type: 'robot_on'; target: { object: string }; feet?: number; stable_s?: number }
  | { type: 'all'; goals: RobotLabGoal[] }
export type RobotLabObject = {
  id: string
  name?: string
  shape: 'sphere' | 'box' | 'cylinder' | 'capsule'
  size: number[]
  pos: number[]
  yaw?: number
  color?: string
  dynamic?: boolean
  mass?: number
  tags?: string[]
}
export type RobotLabEnvironment = {
  version: 1
  id: string
  name: string
  kind: 'room' | 'table'
  description?: string
  size?: number[]
  wall_height?: number
  floor_color?: string
  wall_color?: string
  walls?: { from: number[]; to: number[]; height?: number; thickness?: number; color?: string }[]
  objects: RobotLabObject[]
  spawn?: { x?: number; y?: number; yaw?: number }
  source?: { type: 'text'; notes?: string }
  created_at?: string
  updated_at?: string
}
export type RobotLabAttempt = {
  n: number
  status: 'running' | 'succeeded' | 'failed' | 'abandoned' | 'stopped'
  reason: string
  started_at: string
  ended_at: string | null
  sim_time: number
  frames: number
  skills_used: string[]
}
export type RobotLabSession = {
  id: string
  name: string
  robot: 'arm' | 'dog'
  environment: string
  task: string
  goal: RobotLabGoal
  status: 'live' | 'succeeded' | 'stopped' | 'exhausted' | 'failed'
  created_at: string
  ended_at: string | null
  max_attempts: number
  attempt: number
  attempts: RobotLabAttempt[]
  conversation: string | null
  skill_events: {
    t: string
    attempt: number
    kind: 'used' | 'created' | 'improved' | 'copied'
    skill: string
    version: number
  }[]
  outcome: { status: string; attempt: number; detail: string } | null
}
export type RobotLabSessionSummary = {
  id: string
  name: string
  robot: 'arm' | 'dog'
  environment: string
  task: string
  status: 'live' | 'succeeded' | 'stopped' | 'exhausted' | 'failed'
  created_at: string
  ended_at: string | null
  attempts: number
  live: boolean
}
export type RobotLabSkillSummary = {
  name: string
  description: string
  version: number
  success_rate: number | null
  uses: number
  failures: number
  updated_at: string
}
export type RobotLabSkill = {
  name: string
  description: string
  version: number
  success_rate: number | null
  uses: number
  failures: number
  updated_at: string
  instructions: string
  versions: {
    version: number
    created_at: string
    accepted: boolean
    success_rate: number | null
    uses: number
  }[]
}
export type RobotLabLogEntry = {
  id: string
  t: string
  session: string
  attempt: number
  kind: 'act' | 'look' | 'skill' | 'note' | 'plan' | 'reflection' | 'system'
  tool?: string
  args?: Record<string, unknown>
  ok?: boolean
  summary: string
  image?: string
  sim_time: number
  state?: Record<string, unknown>
}
export type RobotLabFrame = {
  session: string
  attempt: number
  seq: number
  t: number
  poses: number[]
  camera: { name: string; pose: number[] }
  held: string | null
  status: string
}
export type RobotLabBodyCard = {
  id: 'arm' | 'dog'
  name: { en: string; es: string }
  description: string
  sensors: string[]
  cameras: string[]
  tools: { name: string; description: string; params: Record<string, unknown> }[]
  limits: Record<string, unknown>
  notes: string[]
  [key: string]: unknown
}
export type RobotLabScene = {
  version: 1
  bodies: {
    name: string
    geoms: {
      type: 'mesh' | 'box' | 'sphere' | 'cylinder' | 'capsule' | 'plane'
      mesh?: string
      pos?: number[]
      quat?: number[]
      rgba?: number[]
      scale?: number[]
      size?: number[]
    }[]
  }[]
  objects: { id: string; name: string; body: number; dynamic: boolean }[]
  cameras: { name: string; fovy: number }[]
  bounds: { min: number[]; max: number[] }
  robot: 'arm' | 'dog' | null
  environment: string
}
export type RobotLabActionResult = {
  ok: boolean
  result: unknown
  state: Record<string, unknown>
  attempt: { n: number; status: string; reason: string }
  goal: { met: boolean; detail: string }
  skill?: string
  returned?: unknown
}
export type RobotLabStatus = {
  enabled: boolean
  installed: boolean
  python: string
  version?: string
  running: boolean
  error?: string
  requirements: { python: string; osmesa: boolean }
  live_session?: string | null
  robots?: number
  environments?: number
}
export type RobotLabSettings = { max_attempts: number; sim_speed: number }
export type RobotLabVisibleObject = {
  id: string
  name: string
  position?: number[]
  distance: number
  bearing_deg: number
  elevation_deg: number
  pixels: number
}
export interface RpcMethods {
  'robotlab.status': { params: Record<string, never>; result: RobotLabStatus }
  'robotlab.install': { params: Record<string, never>; result: { started: true } }
  'robotlab.robots.list': { params: Record<string, never>; result: { robots: RobotLabBodyCard[] } }
  'robotlab.env.list': {
    params: Record<string, never>
    result: {
      environments: {
        id: string
        name: string
        kind: 'room' | 'table'
        objects: number
        updated_at: string
        source_type: string
      }[]
    }
  }
  'robotlab.env.get': { params: { id: string }; result: { environment: RobotLabEnvironment } }
  'robotlab.env.delete': { params: { id: string }; result: Record<string, never> }
  'robotlab.env.thumbnail': { params: { id: string }; result: { png: string } }
  'robotlab.env.scene': { params: { id: string }; result: { scene: RobotLabScene } }
  'robotlab.env.preview': { params: { id: string; robot?: 'arm' | 'dog' }; result: { png: string } }
  'robotlab.env.save': {
    params: { environment: RobotLabEnvironment; create: boolean }
    result: { environment: RobotLabEnvironment }
  }
  'robotlab.settings.get': { params: Record<string, never>; result: RobotLabSettings }
  'robotlab.settings.set': {
    params: { max_attempts?: number; sim_speed?: number }
    result: RobotLabSettings
  }
  'robotlab.session.start': {
    params: {
      robot: 'arm' | 'dog'
      environment: string
      task: string
      goal: RobotLabGoal
      name?: string
      copy_skills_from?: string
      conversation?: string | null
    }
    result: { session: RobotLabSession; card: RobotLabBodyCard; skills: RobotLabSkillSummary[] }
  }
  'robotlab.session.list': {
    params: { robot?: 'arm' | 'dog'; environment?: string; query?: string }
    result: { sessions: RobotLabSessionSummary[] }
  }
  'robotlab.session.get': { params: { session: string }; result: { session: RobotLabSession } }
  'robotlab.session.stop': {
    params: { session: string; by: 'user' | 'agent'; reason?: string }
    result: { session: RobotLabSession }
  }
  'robotlab.session.scene': { params: { session: string }; result: { scene: RobotLabScene } }
  'robotlab.session.image': { params: { session: string; path: string }; result: { png: string } }
  'robotlab.session.log': {
    params: { session: string; attempt?: number; since?: number }
    result: { entries: RobotLabLogEntry[]; next: number }
  }
  'robotlab.attempt.reset': {
    params: { session: string; reflection: string; no_skill_reason?: string }
    result: { session: RobotLabSession; attempt: number }
  }
  'robotlab.attempt.frames': {
    params: { session: string; attempt: number; from: number; count: number }
    result: { frames: RobotLabFrame[]; total: number }
  }
  'robotlab.robot.act': {
    params: { session: string; tool: string; args: Record<string, unknown> }
    result: RobotLabActionResult
  }
  'robotlab.robot.look': {
    params: { session: string; camera?: string; width?: number; height?: number }
    result: { png: string; visible: RobotLabVisibleObject[]; state: Record<string, unknown> }
  }
  'robotlab.robot.state': {
    params: { session: string }
    result: { state: Record<string, unknown>; held: string | null; attempt: unknown; goal: unknown }
  }
  'robotlab.skills.list': {
    params: { session: string }
    result: { skills: RobotLabSkillSummary[] }
  }
  'robotlab.skills.read': {
    params: { session: string; name: string }
    result: { skill: RobotLabSkill }
  }
  'robotlab.skills.use': {
    params: { session: string; name: string }
    result: { skill: RobotLabSkill }
  }
  'robotlab.skills.write': {
    params: {
      session: string
      name: string
      description: string
      instructions: string
      failures?: number[]
    }
    result: { skill: RobotLabSkill; accepted: true }
  }
  'robotlab.skills.copy': { params: { from: string; to: string }; result: { copied: string[] } }
  'robotlab.log.note': {
    params: { session: string; text: string; kind: 'note' | 'plan' | 'reflection' }
    result: { entry: RobotLabLogEntry }
  }
  'robotlab.mesh.get': {
    params: { key: string }
    result: { key: string; positions: string; indices: string; vertices: number; faces: number }
  }
  'robotlab.session.resume': {
    params: { session: string; reason?: string; conversation?: string | null }
    result: { session: RobotLabSession; card: RobotLabBodyCard; skills: RobotLabSkillSummary[] }
  }
}
export interface RpcEvents {
  'robotlab.frame': RobotLabFrame
  'robotlab.session': { session: RobotLabSession }
  'robotlab.log': { entry: RobotLabLogEntry }
  'robotlab.focus': { session: string; robot: 'arm' | 'dog'; environment: string }
  'robotlab.install': {
    phase: string
    message: string
    percent: number
    done?: boolean
    error?: string
  }
}
// END GENERATED ROBOT LAB

/** Robot Lab JSON wire shapes. Coordinates are MuJoCo z-up, quaternions wxyz. */
export type Vec3 = [number, number, number]
export type Quat = [number, number, number, number]
export type Text = { en: string; es: string }
export type Params = Record<string, unknown>
export interface JSONSchema {
  type?: string
  title?: string
  description?: string
  properties?: Record<string, JSONSchema>
  enum?: (string | number)[]
  default?: unknown
  minimum?: number
  maximum?: number
  required?: string[]
}
export interface Actuator {
  name: string
  label: Text
  kind: 'torque' | 'position' | 'velocity'
  range: [number, number]
  key_neg?: string
  key_pos?: string
}
export interface RewardTerm {
  name: string
  weight: number
  description: Text
}
export interface ScenarioInfo {
  id: string
  title: Text
  description: Text
  category: 'classic' | 'manipulation' | 'locomotion' | 'flight'
  params_schema: JSONSchema
  default_params: Params
  actuators: Actuator[]
  reward_terms: RewardTerm[]
  algorithms: string[]
  default_total_steps: number
  inputs: ('sliders' | 'keyboard' | 'gamepad' | 'pose')[]
  has_pretrained: boolean
  has_vision: boolean
  thumbnail_color: string
}
export interface Geom {
  body: number
  type: 'plane' | 'box' | 'sphere' | 'capsule' | 'cylinder' | 'ellipsoid'
  size: Vec3
  pos: Vec3
  quat: Quat
  rgba: [number, number, number, number]
  name?: string
}
export interface Scene {
  scenario: string
  params: Params
  timestep: number
  bodies: { name: string; parent: number }[]
  geoms: Geom[]
  cameras: { name: string; pos: Vec3; quat: Quat; fovy: number }[]
  actuators: Actuator[]
  camera_default: { pos: Vec3; target: Vec3 }
}
export type Mode = 'idle' | 'manual' | 'policy' | 'pose' | 'paused'
export type BodyPose = [number, number, number, number, number, number, number]
export interface Frame {
  session: string
  preview?: boolean
  t: number
  step: number
  episode: number
  bodies: BodyPose[]
  reward: number
  episode_return: number
  terminated: boolean
  info: Record<string, unknown>
  mode: Mode
}
export type Landmark = [number, number, number, number]
export interface Pose {
  landmarks: Landmark[]
  world?: Vec3[]
  hands?: { handedness: 'Left' | 'Right'; landmarks: Vec3[] }[]
  t: number
  arm?: 'left' | 'right'
  calibration?: { landmarks: Landmark[]; world?: Vec3[] }
}
export interface Mark {
  id: number
  name: string
  label: string
  bbox: [number, number, number, number]
  world: Vec3
}
export type ExperimentStatus = 'draft' | 'running' | 'done' | 'stopped' | 'failed'
export interface Run {
  id: string
  label: string
  algorithm: string
  hyperparams: Params
  status: string
  steps: number
  best_return?: number
  last_return?: number
  wall_s: number
  checkpoints: ('best' | 'last')[]
}
export interface Experiment {
  id: string
  name: string
  scenario: string
  params: Params
  status: ExperimentStatus
  created_at: string
  started_at?: string
  ended_at?: string
  total_steps: number
  reward_weights: Record<string, number>
  runs: Run[]
  code: string
  error?: string
}
export type ExperimentSummary = Omit<Experiment, 'code'>
export interface MetricPoint {
  run: string
  step: number
  wall_s: number
  episode_return?: number
  episode_length?: number
  loss?: number
  extra?: Record<string, unknown>
}
export interface VisionDecision {
  objects: { mark: number; class: string; reason: string }[]
  action: { mark: number; target: string } | null
  explanation: string
  verified?: boolean
  model_verified?: boolean
  verify_note?: string
}
export interface VisionEvent {
  session: string
  step_id: string
  phase: 'see' | 'think' | 'act' | 'verify' | 'done' | 'error'
  image_png_b64?: string
  marks?: Pick<Mark, 'id' | 'bbox'>[]
  decision?: VisionDecision
  detail?: string
  latency_ms?: number
  model?: string
  cost_usd?: number
}
export interface Status {
  enabled: boolean
  installed: boolean
  python: string
  version?: string
  running: boolean
  error?: string
  requirements: { python: string; osmesa: boolean }
}
export interface InstallEvent {
  phase: string
  message: string
  percent?: number
  done?: boolean
  error?: string
}
export interface FocusEvent {
  session?: string
  experiment?: string
  view: 'scene' | 'experiment' | 'vision' | 'teleop'
}
export interface CreateExperiment {
  name: string
  scenario: string
  params?: Params
  runs: { label: string; algorithm: string; hyperparams?: Params }[]
  total_steps?: number
  reward_weights?: Record<string, number>
  code?: string
}
type Method<P, R> = { params: P; result: R }
type Session = { session: string }
type ExperimentID = { experiment: string }
export interface RobotLabMethods {
  'robotlab.status': Method<Record<string, never>, Status>
  'robotlab.install': Method<Record<string, never>, { started: true }>
  'robotlab.scenarios': Method<Record<string, never>, { scenarios: ScenarioInfo[] }>
  'robotlab.session.open': Method<
    { scenario: string; params?: Params; seed?: number },
    { session: string; scene: Scene }
  >
  'robotlab.session.close': Method<Session, Record<string, never>>
  'robotlab.session.reset': Method<Session & { seed?: number }, { scene: Scene }>
  'robotlab.session.params': Method<Session & { params: Params }, { scene: Scene }>
  'robotlab.session.control': Method<
    Session & { mode: Mode; inputs?: Record<string, number>; pose?: Pose },
    { mode: Mode }
  >
  'robotlab.session.policy': Method<
    Session &
      ({ experiment: string; run: string; checkpoint?: 'best' | 'last' } | { pretrained: true }),
    { mode: 'policy' }
  >
  'robotlab.session.snapshot': Method<
    Session & { camera?: string; width?: number; height?: number; marks?: boolean },
    { image_png_b64: string; width: number; height: number; marks: Mark[] }
  >
  'robotlab.experiment.list': Method<Record<string, never>, { experiments: ExperimentSummary[] }>
  'robotlab.experiment.get': Method<ExperimentID, Experiment>
  'robotlab.experiment.create': Method<CreateExperiment, Experiment>
  'robotlab.experiment.code': Method<
    ExperimentID & { code: string },
    { ok: boolean; errors?: string[] }
  >
  'robotlab.experiment.start': Method<ExperimentID, Experiment>
  'robotlab.experiment.stop': Method<ExperimentID, Experiment>
  'robotlab.experiment.metrics': Method<
    ExperimentID & { run?: string; since?: number },
    { points: MetricPoint[]; next: number }
  >
  'robotlab.teleop.mapping': Method<
    Session & { code?: string | null },
    { ok: boolean; errors?: string[] }
  >
  'robotlab.teleop.get': Method<Session, { source: 'default' | 'custom'; code: string }>
  'robotlab.vision.step': Method<Session & { instruction: string }, VisionDecision>
}
export interface RobotLabEvents {
  'robotlab.teleop': { session: string; error?: string; code?: string | null }
  'robotlab.frame': Frame
  'robotlab.metrics': { experiment: string; points: MetricPoint[] }
  'robotlab.experiment': Experiment
  'robotlab.log': { source: string; line: string }
  'robotlab.install': InstallEvent
  'robotlab.vision': VisionEvent
  'robotlab.focus': FocusEvent
}

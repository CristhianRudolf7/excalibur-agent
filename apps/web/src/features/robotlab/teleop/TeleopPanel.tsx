import { useEffect, useRef, useState, type ReactNode } from 'react'
import { RunStatus, RunToggle } from '../RunState'
import { useLabText } from '../i18n'
import { PanelSkeleton } from '@/features/viewer/PanelSkeleton'
import type { Pose, Scene } from './contracts'
import { robotlabCall, robotlabEvents } from './api'
import { confidence, PoseGate, smoothPose, toPose } from './pose'
import { createTracker, type Tracker, type TrackerFactory } from './tracker'
import { drawOverlay } from './overlay'
import { MappingEditor } from './MappingEditor'
import { useRobotText } from './i18n'
import './capture.css'
export interface TeleopPanelProps {
  session: string
  scene: Scene
  sendPose: (pose: Pose) => void
  viewer: ReactNode
  trackerFactory?: TrackerFactory
}
export function TeleopPanel(props: TeleopPanelProps) {
  return <SessionTeleop key={props.session} {...props} />
}

function SessionTeleop({
  session,
  scene,
  sendPose,
  viewer,
  trackerFactory = createTracker,
}: TeleopPanelProps) {
  const text = useRobotText()
  const { text: labText } = useLabText()
  const video = useRef<HTMLVideoElement>(null),
    canvas = useRef<HTMLCanvasElement>(null)
  const moving = useRef(false)
  const enabled = import.meta.env.EXCALIBUR_WEB_ROBOTLAB_TELEOP !== '0'
  const active = useRef<{
    stream?: MediaStream
    tracker?: Tracker
    frame?: number
    generation: number
  }>({ generation: 0 })
  const options = useRef({
    arm: 'right' as 'left' | 'right',
    smoothing: 0.35,
    holdRequired: true,
    held: false,
    calibration: undefined as Pose['calibration'],
    calibrateAt: 0,
    sendPose,
  })
  options.current.sendPose = sendPose
  const [state, setState] = useState<'idle' | 'preparing' | 'tracking' | 'stopping'>('idle')
  const [devices, setDevices] = useState<MediaDeviceInfo[]>([]),
    [device, setDevice] = useState('')
  const [error, setError] = useState(''),
    [mirror, setMirror] = useState(true)
  const [arm, setArm] = useState<'left' | 'right'>('right'),
    [smoothing, setSmoothing] = useState(0.35)
  const [holdRequired, setHoldRequired] = useState(true),
    [held, setHeld] = useState(false)
  const [stats, setStats] = useState({ confidence: 0, fps: 0 }),
    [countdown, setCountdown] = useState(0)
  const [calibrated, setCalibrated] = useState(false)
  function haltMotion() {
    if (!moving.current) return
    moving.current = false
    void idle().catch((e) => setError(String(e)))
  }
  function release() {
    options.current.held = false
    setHeld(false)
    haltMotion()
  }
  function cleanup() {
    moving.current = false
    active.current.generation++
    if (active.current.frame) cancelAnimationFrame(active.current.frame)
    active.current.stream?.getTracks().forEach((track) => track.stop())
    active.current.tracker?.close()
    active.current.stream = undefined
    active.current.tracker = undefined
    options.current.calibrateAt = 0
    options.current.calibration = undefined
  }
  async function idle() {
    await robotlabCall('robotlab.session.control', { session, mode: 'idle' })
  }
  function stop() {
    cleanup()
    release()
    setState('stopping')
    setCountdown(0)
    setCalibrated(false)
    if (canvas.current) drawOverlay(canvas.current, null)
    void idle()
      .catch((e) => setError(String(e)))
      .finally(() => setState('idle'))
  }
  useEffect(() => {
    if (!enabled) return
    const keyDown = (e: KeyboardEvent) => {
      if (
        e.code !== 'Space' ||
        (e.target as HTMLElement)?.closest(
          'input,textarea,select,button,[contenteditable="true"],.monaco-editor',
        )
      )
        return
      e.preventDefault()
      if (e.repeat) return
      options.current.held = true
      setHeld(true)
    }
    const keyUp = (e: KeyboardEvent) => {
      if (e.code === 'Space') release()
    }
    const hide = () => {
      if (document.hidden) stop()
    }
    window.addEventListener('keydown', keyDown)
    window.addEventListener('keyup', keyUp)
    window.addEventListener('blur', release)
    document.addEventListener('visibilitychange', hide)
    const unsubscribe = robotlabEvents('robotlab.teleop', session, (event) => {
      if (typeof event.error === 'string') setError(event.error)
    })
    return () => {
      window.removeEventListener('keydown', keyDown)
      window.removeEventListener('keyup', keyUp)
      window.removeEventListener('blur', release)
      document.removeEventListener('visibilitychange', hide)
      unsubscribe()
      cleanup()
      void idle().catch(() => {})
    }
    // Session owns all camera resources and event listeners.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [session, enabled])
  async function start() {
    cleanup()
    setError('')
    setState('preparing')
    setCalibrated(false)
    setCountdown(0)
    const generation = active.current.generation
    let stream: MediaStream | undefined, tracker: Tracker | undefined
    try {
      stream = await navigator.mediaDevices.getUserMedia({
        audio: false,
        video: device
          ? { deviceId: { exact: device } }
          : { facingMode: 'user', width: { ideal: 640 }, height: { ideal: 480 } },
      })
      if (generation !== active.current.generation) {
        stream.getTracks().forEach((t) => t.stop())
        return
      }
      active.current.stream = stream
      stream.getVideoTracks()[0]?.addEventListener('ended', stop, { once: true })
      setDevices(
        (await navigator.mediaDevices.enumerateDevices()).filter((d) => d.kind === 'videoinput'),
      )
      if (generation !== active.current.generation) return
      if (!video.current) throw new Error('Camera preview unavailable')
      video.current.srcObject = stream
      await video.current.play()
      if (generation !== active.current.generation) return
      tracker = await trackerFactory()
      if (generation !== active.current.generation) {
        tracker.close()
        return
      }
      active.current.tracker = tracker
      setState('tracking')
      const gate = new PoseGate()
      let previous: Pose | undefined,
        lastVideo = -1,
        lastStats = 0,
        lastFrame = performance.now()
      const tick = async (now: number) => {
        if (generation !== active.current.generation) return
        try {
          if (
            video.current &&
            video.current.readyState >= 2 &&
            lastVideo !== video.current.currentTime
          ) {
            lastVideo = video.current.currentTime
            const result = await tracker!.detect(video.current, now)
            if (generation !== active.current.generation) return
            const raw = toPose(result, now, options.current.arm)
            const score = raw ? confidence(raw) : 0
            const pose =
              raw && score >= 0.65 ? smoothPose(raw, previous, options.current.smoothing) : null
            previous = pose ?? undefined
            if (canvas.current) {
              canvas.current.width = video.current.videoWidth
              canvas.current.height = video.current.videoHeight
              drawOverlay(canvas.current, pose)
            }
            if (options.current.calibrateAt) {
              const remaining = Math.max(0, Math.ceil((options.current.calibrateAt - now) / 1000))
              setCountdown(remaining)
              if (!remaining) {
                options.current.calibrateAt = 0
                if (raw && score >= 0.65) {
                  options.current.calibration = { landmarks: raw.landmarks, world: raw.world }
                  setCalibrated(true)
                } else setError(text('waiting'))
              }
            }
            if (
              !pose ||
              options.current.calibrateAt ||
              (options.current.holdRequired && !options.current.held)
            )
              haltMotion()
            if (
              pose &&
              gate.next(
                pose,
                options.current.held,
                options.current.holdRequired,
                performance.now(),
              ) &&
              !options.current.calibrateAt
            ) {
              moving.current = true
              options.current.sendPose({ ...pose, calibration: options.current.calibration })
            }
            if (now - lastStats > 250) {
              setStats({ confidence: score, fps: Math.round(1000 / Math.max(1, now - lastFrame)) })
              lastStats = now
            }
            lastFrame = now
          }
          active.current.frame = requestAnimationFrame(tick)
        } catch (e) {
          if (generation !== active.current.generation) return
          stop()
          setError(String(e))
        }
      }
      active.current.frame = requestAnimationFrame(tick)
    } catch (e) {
      stream?.getTracks().forEach((t) => t.stop())
      tracker?.close()
      if (generation === active.current.generation) {
        cleanup()
        setState('idle')
        setError(String(e))
      }
    }
  }
  if (!enabled) return <p>{text('disabled')}</p>
  return (
    <section className="robot-capture" aria-label={text('title')} data-scenario={scene.scenario}>
      <header>
        <h2>{text('title')}</h2>
        <RunStatus
          state={
            state === 'preparing'
              ? 'starting'
              : state === 'stopping'
                ? 'stopping'
                : state === 'tracking'
                  ? 'running'
                  : error
                    ? 'error'
                    : 'stopped'
          }
          surface="teleop"
        />
        <RunToggle
          active={state === 'tracking'}
          stop
          busy={state === 'preparing' || state === 'stopping'}
          label={labText(
            state === 'preparing'
              ? 'runStarting'
              : state === 'stopping'
                ? 'runStopping'
                : state === 'tracking'
                  ? 'cameraDisable'
                  : 'cameraEnable',
          )}
          onClick={() => (state === 'tracking' ? stop() : void start())}
        />
      </header>
      <p>{text('privacy')}</p>
      <div className="robot-capture-pair">
        <div className="robot-camera">
          <div className={mirror ? 'robot-camera-image mirrored' : 'robot-camera-image'}>
            <video ref={video} muted playsInline />
            <canvas ref={canvas} aria-label={text('ready')} />
          </div>
          {state === 'preparing' && <PanelSkeleton shape="canvas" label={text('preparing')} />}
        </div>
        <div>{viewer}</div>
      </div>
      <p role="status">
        {state === 'tracking' ? text(stats.confidence >= 0.65 ? 'ready' : 'waiting') : text('idle')}{' '}
        · {text('confidence')} {Math.round(stats.confidence * 100)}% · {stats.fps} FPS
      </p>
      <div className="robot-capture-controls">
        <label>
          {text('camera')}
          <select
            value={device}
            disabled={state !== 'idle'}
            onChange={(e) => setDevice(e.target.value)}
          >
            <option value="">{text('camera')}</option>
            {devices.map((d) => (
              <option key={d.deviceId} value={d.deviceId}>
                {d.label || d.deviceId}
              </option>
            ))}
          </select>
        </label>

        <label>
          {text('arm')}
          <select
            value={arm}
            onChange={(e) => {
              const value = e.target.value as 'left' | 'right'
              setArm(value)
              options.current.arm = value
            }}
          >
            <option value="left">{text('left')}</option>
            <option value="right">{text('right')}</option>
          </select>
        </label>
        <label>
          <input type="checkbox" checked={mirror} onChange={(e) => setMirror(e.target.checked)} />
          {text('mirror')}
        </label>
        <label>
          {text('smoothing')}
          <input
            type="range"
            min="0"
            max="0.9"
            step="0.05"
            value={smoothing}
            onChange={(e) => {
              setSmoothing(+e.target.value)
              options.current.smoothing = +e.target.value
            }}
          />
        </label>
        <button
          disabled={state !== 'tracking' || countdown > 0}
          onClick={() => {
            options.current.calibrateAt = performance.now() + 3000
            setCountdown(3)
            setCalibrated(false)
          }}
        >
          {countdown || text('calibrate')}
        </button>
        {calibrated && <span>{text('calibrated')}</span>}
        <label>
          <input
            type="checkbox"
            checked={holdRequired}
            onChange={(e) => {
              setHoldRequired(e.target.checked)
              options.current.holdRequired = e.target.checked
              release()
            }}
          />
          {text('hold')}
        </label>
        <button
          disabled={state !== 'tracking'}
          aria-pressed={held}
          onPointerDown={(e) => {
            e.currentTarget.setPointerCapture(e.pointerId)
            options.current.held = true
            setHeld(true)
          }}
          onPointerUp={release}
          onPointerCancel={release}
          onLostPointerCapture={release}
          onBlur={release}
          onKeyDown={(e) => {
            if (e.code === 'Space' || e.code === 'Enter') {
              e.preventDefault()
              options.current.held = true
              setHeld(true)
            }
          }}
          onKeyUp={release}
        >
          {text('move')}
        </button>
      </div>
      {error && <p role="alert">{error}</p>}
      <MappingEditor session={session} />
    </section>
  )
}
export default TeleopPanel

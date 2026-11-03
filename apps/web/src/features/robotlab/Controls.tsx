import { SceneRunControls } from './SceneRunControls'
import { useCallback, useEffect, useRef, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Segmented } from '@/features/settings/Segmented'
import type { Frame, Mode, Params, ScenarioInfo } from './types'
import { useLab, type LabSession } from './state'
import { labCall, useRobotEvent } from './api'
import { inputThrottle } from './logic'
import { ParamsForm, RewardWeights } from './ParamsForm'
import { useSceneRun, useSceneRuns } from './sceneRun'
import { useLabText } from './i18n'
export function Controls({
  active,
  scenario,
  onScene,
  weights,
  onWeights,
  onError,
}: {
  active: LabSession
  scenario: ScenarioInfo
  onScene: (value: LabSession) => void
  weights: Record<string, number>
  onWeights: (weights: Record<string, number>) => void
  onError: (error: unknown) => void
}) {
  const { text, language } = useLabText()
  const { mode, previous, setMode } = useSceneRun(active.session)
  const [inputs, setInputs] = useState<Record<string, number>>({})
  const [device, setDevice] = useState<'sliders' | 'keyboard' | 'gamepad' | 'noInput'>('noInput')
  const [frame, setFrame] = useState<Frame>()
  const [params, setParams] = useState<Params>(active.scene.params)
  const modeRef = useRef<Mode>('idle')
  useEffect(() => {
    modeRef.current = mode
  }, [mode])
  const values = useRef<Record<string, number>>({})
  const sendControl = useCallback(
    (control: { mode: Mode; inputs?: Record<string, number> }) => {
      if (useSceneRuns.getState().session === active.session && useSceneRuns.getState().run.pending)
        return
      void labCall('robotlab.session.control', { session: active.session, ...control })
        .then((result) => {
          modeRef.current = result.mode
          setMode(result.mode)
        })
        .catch((error: unknown) => onError(error))
    },
    [active.session, onError, setMode],
  )
  const sender = useRef<ReturnType<
    typeof inputThrottle<{
      mode: Mode
      inputs?: Record<string, number>
    }>
  > | null>(null)
  useEffect(() => {
    const throttle = inputThrottle(sendControl)
    sender.current = throttle
    return () => {
      throttle.cancel()
      sender.current = null
      if (modeRef.current === 'manual' && useLab.getState().active?.session === active.session)
        void labCall('robotlab.session.control', {
          session: active.session,
          mode: 'manual',
          inputs: Object.fromEntries(active.scene.actuators.map((actuator) => [actuator.name, 0])),
        }).catch(onError)
    }
  }, [sendControl, active.session, active.scene.actuators, onError])
  useRobotEvent('robotlab.frame', (event) => {
    if (event.session === active.session && !event.preview) {
      setFrame(event)
      setMode(event.mode)
      modeRef.current = event.mode
    }
  })
  const update = useCallback(
    (next: Record<string, number>, source: typeof device) => {
      if (useSceneRuns.getState().run.pending) return
      values.current = next
      setInputs(next)
      setDevice(Object.values(next).some((value) => value !== 0) ? source : 'noInput')
      modeRef.current = 'manual'
      setMode('manual')
      sender.current?.push({ mode: 'manual', inputs: next })
    },
    [setMode],
  )
  useEffect(() => {
    if (mode === 'paused') sender.current?.cancel()
  }, [mode])
  useEffect(() => {
    const held = new Set<string>()
    const keyboard = (event: KeyboardEvent) => {
      const target = event.target instanceof HTMLElement ? event.target : null
      if (
        target?.closest('input,textarea,[contenteditable="true"],dialog') ||
        event.ctrlKey ||
        event.metaKey ||
        event.altKey
      )
        return
      if (modeRef.current === 'paused') return
      if (
        !active.scene.actuators.some(
          (actuator) => actuator.key_neg === event.code || actuator.key_pos === event.code,
        )
      )
        return
      event.preventDefault()
      if (event.type === 'keydown') held.add(event.code)
      else held.delete(event.code)
      const next = { ...values.current }
      for (const actuator of active.scene.actuators)
        next[actuator.name] =
          Number(held.has(actuator.key_pos ?? '')) - Number(held.has(actuator.key_neg ?? ''))
      update(next, 'keyboard')
    }
    const neutral = () => {
      held.clear()
      if (modeRef.current === 'manual')
        update(
          Object.fromEntries(active.scene.actuators.map((actuator) => [actuator.name, 0])),
          'noInput',
        )
    }
    const visibility = () => {
      if (document.hidden) neutral()
    }
    window.addEventListener('keydown', keyboard)
    window.addEventListener('keyup', keyboard)
    window.addEventListener('blur', neutral)
    document.addEventListener('visibilitychange', visibility)
    let hadGamepad = false
    let previous = ''
    const timer = setInterval(() => {
      if (document.hidden || modeRef.current !== 'manual' || !scenario.inputs.includes('gamepad'))
        return
      const pad = navigator.getGamepads?.().find((item) => item?.connected)
      if (!pad) {
        if (hadGamepad) neutral()
        hadGamepad = false
        previous = ''
        return
      }
      const next = Object.fromEntries(
        active.scene.actuators.map((actuator, i) => {
          const axis = pad.axes[i] ?? 0
          return [actuator.name, Math.abs(axis) < 0.12 ? 0 : Math.round(axis * 100) / 100]
        }),
      )
      const signature = JSON.stringify(next)
      if (signature !== previous && (hadGamepad || Object.values(next).some(Boolean)))
        update(next, 'gamepad')
      previous = signature
      hadGamepad = true
    }, 1000 / 30)
    return () => {
      if (useLab.getState().active?.session === active.session) neutral()
      clearInterval(timer)
      window.removeEventListener('keydown', keyboard)
      window.removeEventListener('keyup', keyboard)
      window.removeEventListener('blur', neutral)
      document.removeEventListener('visibilitychange', visibility)
    }
  }, [active.session, active.scene.actuators, scenario.inputs, update])
  return (
    <div className="lab-controls">
      <SceneRunControls
        session={active.session}
        onError={onError}
        shortcut={!active.scene.actuators.some((a) => a.key_neg === 'KeyP' || a.key_pos === 'KeyP')}
      />
      <Segmented<Mode | 'pretrained'>
        label={text('controls')}
        value={mode === 'paused' ? previous : mode}
        options={(
          [
            'idle',
            'manual',
            'policy',
            ...(scenario.has_pretrained ? (['pretrained'] as const) : []),
          ] as (Mode | 'pretrained')[]
        ).map((value) => ({ value, label: text(value === 'pose' ? 'teleop' : value) }))}
        onChange={(next) => {
          if (next === 'pretrained') {
            void labCall('robotlab.session.policy', { session: active.session, pretrained: true })
              .then(() => {
                modeRef.current = 'policy'
                setMode('policy')
              })
              .catch(onError)
          } else {
            modeRef.current = next
            setMode(next)
            sender.current?.cancel()
            sendControl({ mode: next })
          }
        }}
      />
      <p className="lab-input-indicator" role="status">
        <i data-active={device !== 'noInput'} />
        {text(device)}
      </p>
      <div className="lab-hud">
        {(['step', 'episode', 'return', 'reward'] as const).map((name) => (
          <div key={name}>
            <span>{text(name)}</span>
            <strong>
              {name === 'return'
                ? (frame?.episode_return ?? 0).toFixed(2)
                : name === 'reward'
                  ? (frame?.reward ?? 0).toFixed(2)
                  : (frame?.[name] ?? 0)}
            </strong>
          </div>
        ))}
      </div>
      {scenario.inputs.includes('sliders') &&
        active.scene.actuators.map((actuator) => (
          <label className="lab-slider" key={actuator.name}>
            <span>
              {actuator.label[language]} <output>{(inputs[actuator.name] ?? 0).toFixed(2)}</output>
            </span>
            <input
              aria-label={actuator.label[language]}
              type="range"
              min={-1}
              max={1}
              step={0.01}
              value={inputs[actuator.name] ?? 0}
              onChange={(event) =>
                update(
                  { ...values.current, [actuator.name]: event.target.valueAsNumber },
                  'sliders',
                )
              }
              onPointerUp={() => {
                if (actuator.kind === 'torque')
                  update({ ...values.current, [actuator.name]: 0 }, 'sliders')
              }}
              onPointerCancel={() => {
                if (actuator.kind === 'torque')
                  update({ ...values.current, [actuator.name]: 0 }, 'sliders')
              }}
              onKeyUp={() => {
                if (actuator.kind === 'torque')
                  update({ ...values.current, [actuator.name]: 0 }, 'sliders')
              }}
            />
            <small>
              {actuator.range.join(' … ')} · {actuator.key_neg} / {actuator.key_pos}
            </small>
          </label>
        ))}
      <Button
        onClick={() => {
          void labCall('robotlab.session.reset', { session: active.session })
            .then(({ scene }) => onScene({ ...active, scene }))
            .catch(onError)
        }}
      >
        {text('reset')}
      </Button>
      <form
        onSubmit={(event) => {
          event.preventDefault()
          void labCall('robotlab.session.params', { session: active.session, params })
            .then(({ scene }) => onScene({ ...active, scene }))
            .catch(onError)
        }}
      >
        <h3>{text('params')}</h3>
        <ParamsForm schema={scenario.params_schema} value={params} onChange={setParams} />
        <Button type="submit">{text('apply')}</Button>
      </form>
      <RewardWeights terms={scenario.reward_terms} value={weights} onChange={onWeights} />
    </div>
  )
}

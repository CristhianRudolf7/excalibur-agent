import { useCallback, useEffect, useState } from 'react'
import { create } from 'zustand'
import { labCall, useRobotEvent } from './api'
import type { Mode } from './types'

type SceneRun = {
  mode: Mode
  previous: Exclude<Mode, 'paused'>
  received: number
  pending: boolean
}
const initial: SceneRun = { mode: 'idle', previous: 'idle', received: 0, pending: false }
export const useSceneRuns = create<{ session?: string; run: SceneRun; playback?: string }>(() => ({
  run: initial,
}))
function update(session: string, mode: Mode, received?: number) {
  useSceneRuns.setState((state) => {
    const run = state.session === session ? state.run : initial
    return {
      session,
      playback: mode === 'policy' || mode === 'paused' ? state.playback : undefined,
      run: {
        ...run,
        mode,
        previous: mode === 'paused' ? run.previous : mode,
        received: received ?? run.received,
      },
    }
  })
}
export function useSceneRun(session: string) {
  const run = useSceneRuns((state) => (state.session === session ? state.run : initial))
  const [mountedAt] = useState(Date.now)
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 250)
    return () => clearInterval(timer)
  }, [])
  useRobotEvent('robotlab.frame', (frame) => {
    if (frame.session === session && !frame.preview) update(session, frame.mode, Date.now())
  })
  const setMode = useCallback((mode: Mode) => update(session, mode), [session])
  const control = useCallback(
    async (mode: Mode) => {
      useSceneRuns.setState((state) => ({
        session,
        run: { ...(state.session === session ? state.run : initial), pending: true },
      }))
      try {
        const result = await labCall('robotlab.session.control', { session, mode })
        if (useSceneRuns.getState().session === session) update(session, result.mode)
      } finally {
        useSceneRuns.setState((state) =>
          state.session === session ? { run: { ...state.run, pending: false } } : {},
        )
      }
    },
    [session],
  )
  return {
    ...run,
    setMode,
    control,
    state:
      run.mode === 'paused'
        ? ('paused' as const)
        : now - (run.received || mountedAt) > 2000
          ? ('noSignal' as const)
          : ('running' as const),
  }
}

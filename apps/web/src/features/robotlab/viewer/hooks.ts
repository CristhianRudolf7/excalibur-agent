import { useQuery } from '@tanstack/react-query'
import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useViewerText } from './i18n'
import { advanceTime, clampTime, fetchFrames, frameAt } from './recording'
import { subscribeFrames, viewerCall } from './rpc'
import { transientViewerError, viewerRetryDelay } from './retry'
import type { Frame, Scene } from './types'

export function useScene(
  kind: 'session' | 'env',
  id: string | undefined,
  live = false,
): {
  scene: Scene | null
  loading: boolean
  error?: string
} {
  const text = useViewerText()
  const retryLive = useRef(live)
  useLayoutEffect(() => {
    retryLive.current = live
  }, [live])
  const query = useQuery({
    queryKey: ['robotlab', 'viewer', 'scene', kind, id],
    enabled: !!id,
    retry: (_count, error) => retryLive.current && transientViewerError(error),
    retryDelay: viewerRetryDelay,
    queryFn: ({ signal }) =>
      kind === 'session'
        ? viewerCall('robotlab.session.scene', { session: id! }, { signal })
        : viewerCall('robotlab.env.scene', { id: id! }, { signal }),
  })
  return {
    scene: query.data?.scene ?? null,
    loading: !!id && query.isPending,
    ...(query.isError ? { error: text('sceneError') } : {}),
  }
}

/** Last known pose, including the previous recording when a new attempt is still empty. */
export function useLatestRecordedFrame(session: string | undefined, attempt: number | undefined) {
  const query = useQuery({
    queryKey: ['robotlab', 'viewer', 'latest-frame', session, attempt],
    enabled: !!session && attempt !== undefined,
    retry: 3,
    queryFn: async ({ signal }) => {
      for (let n = attempt!; n >= 1; n--) {
        signal.throwIfAborted()
        const head = await viewerCall(
          'robotlab.attempt.frames',
          { session: session!, attempt: n, from: 0, count: 1 },
          { signal },
        )
        signal.throwIfAborted()
        if (head.total === 0) continue
        const tail =
          head.total <= 1
            ? head
            : await viewerCall(
                'robotlab.attempt.frames',
                { session: session!, attempt: n, from: head.total - 1, count: 1 },
                { signal },
              )
        signal.throwIfAborted()
        const frame = tail.frames.at(-1)
        if (frame && frame.session === session && frame.attempt === n) return frame
      }
      return null
    },
  })
  return query.data ?? null
}

export function useLiveFrame(session: string | undefined, attempt?: number): Frame | null {
  const key = JSON.stringify([session, attempt])
  const [state, setState] = useState<{ key: string; frame: Frame | null }>({
    key,
    frame: null,
  })
  if (state.key !== key) setState({ key, frame: null })
  useEffect(() => {
    if (!session) return
    let pending: Frame | null = null
    let latest: Frame | null = null
    let animation: number | undefined
    const stop = subscribeFrames((frame) => {
      if (frame.session !== session || (attempt !== undefined && frame.attempt !== attempt)) return
      if (
        latest &&
        (frame.attempt < latest.attempt ||
          (frame.attempt === latest.attempt && frame.seq <= latest.seq))
      )
        return
      latest = pending = frame
      animation ??= requestAnimationFrame(() => {
        animation = undefined
        setState({ key, frame: pending })
      })
    })
    return () => {
      stop()
      if (animation !== undefined) cancelAnimationFrame(animation)
    }
  }, [session, attempt, key])
  return state.key === key ? state.frame : null
}

export function useReplay(
  session: string | undefined,
  attempt: number | undefined,
  startAtEnd = false,
) {
  const text = useViewerText()
  const key = JSON.stringify([session, attempt])
  const enabled = !!session && attempt !== undefined
  const recording = useQuery({
    queryKey: ['robotlab', 'viewer', 'frames', session, attempt],
    enabled,
    retry: false,
    queryFn: ({ signal }) => fetchFrames(session!, attempt!, signal),
  })
  const [speed, updateSpeed] = useState(1)
  const initialTime = startAtEnd ? null : 0
  const [state, setState] = useState<{ key: string; t: number | null; playing: boolean }>({
    key,
    t: initialTime,
    playing: false,
  })
  if (state.key !== key) setState({ key, t: initialTime, playing: false })
  const frames = recording.data ?? []
  const duration = frames.at(-1)?.t ?? 0
  const loading = enabled && recording.isPending
  useEffect(() => {
    if (state.key !== key || !state.playing) return
    let animation: number | undefined
    let previous = performance.now()
    const tick = (now: number) => {
      const elapsed = now - previous
      previous = now
      setState((value) => {
        if (value.key !== key || !value.playing) return value
        const t = advanceTime(value.t ?? duration, elapsed, speed, duration)
        return { ...value, t, playing: t < duration }
      })
      animation = requestAnimationFrame(tick)
    }
    animation = requestAnimationFrame(tick)
    return () => {
      if (animation !== undefined) cancelAnimationFrame(animation)
    }
  }, [key, state.key, state.playing, duration, speed])
  const play = useCallback(() => {
    setState((value) =>
      value.key === key && !loading && duration > 0
        ? { ...value, t: (value.t ?? duration) >= duration ? 0 : value.t, playing: true }
        : value,
    )
  }, [key, loading, duration])
  const pause = useCallback(() => setState((value) => ({ ...value, playing: false })), [])
  const seek = useCallback(
    (t: number) =>
      setState((value) => {
        if (value.key !== key) return value
        const next = clampTime(t, duration)
        return { ...value, t: next, playing: value.playing && next < duration }
      }),
    [key, duration],
  )
  const setSpeed = useCallback((value: number) => {
    if (Number.isFinite(value) && value > 0) updateSpeed(Math.max(0.25, Math.min(value, 4)))
  }, [])
  const valid = state.key === key
  const t = valid ? (state.t ?? duration) : 0
  return {
    frame: valid ? frameAt(frames, t) : null,
    t,
    duration,
    playing: valid && state.playing,
    loading,
    ...(recording.isError ? { error: text('replayError') } : {}),
    play,
    pause,
    seek,
    speed,
    setSpeed,
  }
}

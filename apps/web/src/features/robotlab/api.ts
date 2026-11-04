import { useEffect, useRef } from 'react'
import { rpc, type CallOptions } from '@/lib/rpc/client'
import { subscribeEvents } from '@/lib/rpc/events'
import type { RobotLabEvents, RobotLabMethods } from './types'

export const robotlabEnabled = import.meta.env.EXCALIBUR_WEB_ROBOTLAB !== '0'
export function labCall<M extends keyof RobotLabMethods>(
  method: M,
  params: RobotLabMethods[M]['params'],
  options?: CallOptions,
) {
  return rpc.call(method, params, options)
}
// Section 8 is implemented before its generated shared type reaches this branch.
export function sessionImage(params: { session: string; path: string }, options?: CallOptions) {
  const imageClient = rpc as unknown as {
    call(
      method: 'robotlab.session.image',
      params: { session: string; path: string },
      options?: CallOptions,
    ): Promise<{ png: string }>
  }
  return imageClient.call('robotlab.session.image', params, options)
}
type Listener = (type: keyof RobotLabEvents, payload: RobotLabEvents[keyof RobotLabEvents]) => void
const listeners = new Set<Listener>()
let disconnect: (() => void) | undefined
export function onRobotEvent<K extends keyof RobotLabEvents>(
  type: K,
  callback: (event: RobotLabEvents[K]) => void,
) {
  if (!robotlabEnabled) return () => {}
  const listener: Listener = (name, payload) => {
    if (name === type) callback(payload as RobotLabEvents[K])
  }
  listeners.add(listener)
  disconnect ??= subscribeEvents({
    types: ['robotlab.focus', 'robotlab.session', 'robotlab.log', 'robotlab.install'],
    event: ({ type, payload }) => {
      for (const receive of listeners)
        receive(type as keyof RobotLabEvents, payload as RobotLabEvents[keyof RobotLabEvents])
    },
    ready: () => {},
    offline: () => {},
  })
  return () => {
    listeners.delete(listener)
    if (!listeners.size) {
      disconnect?.()
      disconnect = undefined
    }
  }
}
export function useRobotEvent<K extends keyof RobotLabEvents>(
  type: K,
  callback: (event: RobotLabEvents[K]) => void,
) {
  const latest = useRef(callback)
  useEffect(() => {
    latest.current = callback
  }, [callback])
  useEffect(() => onRobotEvent(type, (event) => latest.current(event)), [type])
}

import { useEffect, useRef } from 'react'
import { rpc, type CallOptions } from '@/lib/rpc/client'
import { subscribeEvents } from '@/lib/rpc/events'
import type { RobotLabEvents, RobotLabMethods } from './types'
/** Add feature-owned methods to the existing transport without changing its frozen table. */
declare module '@/lib/rpc/contract' {
  interface RpcMethods extends RobotLabMethods {}
}
export const robotlabEnabled = import.meta.env.EXCALIBUR_WEB_ROBOTLAB !== '0'
export function labCall<M extends keyof RobotLabMethods>(
  method: M,
  params: RobotLabMethods[M]['params'],
  options?: CallOptions,
) {
  // A vision step includes two model decisions and robot motion, unlike ordinary RPCs.
  if (options || method === 'robotlab.vision.step')
    return rpc.call(method, params, {
      ...(method === 'robotlab.vision.step' ? { timeoutMs: 180_000 } : {}),
      ...options,
    })
  return rpc.call(method, params)
}
export const robotlab = {
  call: labCall,
  status: () => labCall('robotlab.status', {}),
  scenarios: () => labCall('robotlab.scenarios', {}),
  control: (params: RobotLabMethods['robotlab.session.control']['params']) =>
    labCall('robotlab.session.control', params),
}
type Listener = (type: keyof RobotLabEvents, payload: RobotLabEvents[keyof RobotLabEvents]) => void
const listeners = new Set<Listener>()
const offlineListeners = new Set<() => void>()
let disconnect: (() => void) | undefined
export function onRobotEvent<K extends keyof RobotLabEvents>(
  type: K,
  callback: (payload: RobotLabEvents[K]) => void,
  offline?: () => void,
) {
  if (!robotlabEnabled) return () => {}
  const listener: Listener = (name, payload) => {
    if (name === type) callback(payload as RobotLabEvents[K])
  }
  listeners.add(listener)
  if (offline) offlineListeners.add(offline)
  disconnect ??= subscribeEvents({
    types: [
      'robotlab.teleop',
      'robotlab.frame',
      'robotlab.metrics',
      'robotlab.experiment',
      'robotlab.log',
      'robotlab.install',
      'robotlab.vision',
      'robotlab.focus',
    ],
    event: ({ type: name, payload }) => {
      for (const receive of listeners)
        receive(name as keyof RobotLabEvents, payload as RobotLabEvents[keyof RobotLabEvents])
    },
    ready: () => {},
    offline: () => {
      for (const notify of offlineListeners) notify()
    },
  })
  return () => {
    listeners.delete(listener)
    if (offline) offlineListeners.delete(offline)
    if (!listeners.size) {
      disconnect?.()
      disconnect = undefined
    }
  }
}
export function useRobotEvent<K extends keyof RobotLabEvents>(
  type: K,
  callback: (payload: RobotLabEvents[K]) => void,
) {
  const latest = useRef(callback)
  useEffect(() => {
    latest.current = callback
  }, [callback])
  useEffect(() => onRobotEvent(type, (event) => latest.current(event)), [type])
}

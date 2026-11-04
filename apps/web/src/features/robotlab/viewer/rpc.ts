import { rpc, type CallOptions } from '@/lib/rpc/client'
import { subscribeEvents } from '@/lib/rpc/events'
import type { RpcMethods } from '@/lib/rpc/contract'
import type { Frame } from './types'

type ViewerMethods = Pick<
  RpcMethods,
  'robotlab.session.scene' | 'robotlab.env.scene' | 'robotlab.mesh.get' | 'robotlab.attempt.frames'
>
export function viewerCall<M extends keyof ViewerMethods>(
  method: M,
  params: ViewerMethods[M]['params'],
  options?: CallOptions,
) {
  return rpc.call(method, params, options)
}

function isFrame(value: unknown): value is Frame {
  if (!value || typeof value !== 'object') return false
  const frame = value as Partial<Frame>
  return (
    typeof frame.session === 'string' &&
    Number.isFinite(frame.attempt) &&
    Number.isFinite(frame.seq) &&
    Number.isFinite(frame.t) &&
    Array.isArray(frame.poses) &&
    frame.poses.every(Number.isFinite) &&
    !!frame.camera &&
    typeof frame.camera.name === 'string' &&
    Array.isArray(frame.camera.pose) &&
    frame.camera.pose.length === 7 &&
    frame.camera.pose.every(Number.isFinite)
  )
}
export function subscribeFrames(receive: (frame: Frame) => void) {
  return subscribeEvents({
    types: ['robotlab.frame'],
    event: ({ type, payload }) => {
      if (type === 'robotlab.frame' && isFrame(payload)) receive(payload)
    },
    ready: () => {},
    offline: () => {},
  })
}

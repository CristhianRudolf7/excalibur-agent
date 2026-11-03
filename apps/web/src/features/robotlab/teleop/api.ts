import { labCall, onRobotEvent } from '../api'
import type { RobotLabEvents } from '../types'

export const robotlabCall = labCall
export function robotlabEvents<K extends 'robotlab.teleop' | 'robotlab.vision'>(
  type: K,
  session: string,
  receive: (data: RobotLabEvents[K]) => void,
  offline?: () => void,
) {
  return onRobotEvent(
    type,
    (data) => {
      if (data.session === session) receive(data)
    },
    offline,
  )
}

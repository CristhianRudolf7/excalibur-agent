import { useLabText } from './i18n'
import { stateRecord } from './robotState'
import type { RobotId } from './types'

export function RobotState({
  robot,
  state,
  time,
}: {
  robot: RobotId
  state?: Record<string, unknown>
  time?: number
}) {
  const { text, language } = useLabText()
  const number = (value: unknown, decimals: number) =>
    typeof value === 'number' && Number.isFinite(value)
      ? new Intl.NumberFormat(language, {
          minimumFractionDigits: decimals,
          maximumFractionDigits: decimals,
        }).format(value)
      : text('unknown')
  const meters = (value: unknown) => `${number(value, 3)} m`
  const degrees = (value: unknown) => `${number(value, 1)}°`
  const gripper = stateRecord(state?.gripper)
  const pos = gripper?.pos
  const opening = gripper?.opening
  const rows =
    robot === 'dog'
      ? [
          [
            text('supportFeet'),
            Array.isArray(state?.support)
              ? state.support.filter((foot) => typeof foot === 'string').join(' · ') ||
                text('noSupport')
              : text('unknown'),
          ],
          [text('stabilityMargin'), meters(state?.margin)],
          [text('trunkHeight'), meters(state?.z)],
          [text('trunkPitch'), degrees(state?.pitch_deg)],
          [text('trunkRoll'), degrees(state?.roll_deg)],
        ]
      : [
          [
            text('gripperPosition'),
            Array.isArray(pos) && pos.length === 3
              ? pos.map((value) => number(value, 3)).join(' / ') + ' m'
              : text('unknown'),
          ],
          [
            text('gripperOrientation'),
            [gripper?.yaw_deg, gripper?.pitch_deg, gripper?.roll_deg].map(degrees).join(' / '),
          ],
          [
            text('gripperOpening'),
            typeof opening === 'number' && Number.isFinite(opening)
              ? new Intl.NumberFormat(language, {
                  style: 'percent',
                  maximumFractionDigits: 0,
                }).format(opening) +
                (typeof gripper?.opening_m === 'number' ? ` · ${meters(gripper.opening_m)}` : '')
              : text('unknown'),
          ],
          [
            text('heldObject'),
            state?.held === null
              ? text('nothingHeld')
              : typeof state?.held === 'string'
                ? state.held
                : text('unknown'),
          ],
        ]
  return (
    <section className="lab-robot-state" aria-label={text('latestState')}>
      <div className="lab-state-heading">
        <strong>{text('latestState')}</strong>
        {time !== undefined && <span>{number(time, 2)} s</span>}
      </div>
      {state ? (
        <dl>
          {rows.map(([label, value]) => (
            <div key={label}>
              <dt>{label}</dt>
              <dd>{value}</dd>
            </div>
          ))}
        </dl>
      ) : (
        <p>{text('noRobotState')}</p>
      )}
    </section>
  )
}

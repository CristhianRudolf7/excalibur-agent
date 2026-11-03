import { Link, useLocation } from '@tanstack/react-router'
import { useRobotEvent, robotlabEnabled } from './api'
import { useLab } from './state'
import { useLabText } from './i18n'
export function FocusConnection() {
  const path = useLocation({ select: (location) => location.pathname })
  const focus = useLab((state) => state.focus)
  const { text } = useLabText()
  useRobotEvent('robotlab.focus', (event) =>
    useLab.setState((state) => ({ focus: event, sequence: state.sequence + 1 })),
  )
  if (!robotlabEnabled || path === '/lab' || !focus) return null
  return (
    <aside className="lab-focus-card" role="status">
      <Link to="/lab">{text('open')}</Link>
      <button aria-label={text('dismiss')} onClick={() => useLab.setState({ focus: undefined })}>
        ×
      </button>
    </aside>
  )
}

import { useNavigate } from '@tanstack/react-router'
import { useQueryClient } from '@tanstack/react-query'
import { useRobotEvent } from './api'
import { useLab } from './state'

export function LabConnection() {
  const navigate = useNavigate()
  const client = useQueryClient()
  useRobotEvent('robotlab.focus', (event) => {
    useLab.getState().focusSession(event.session)
    void client.invalidateQueries({ queryKey: ['robotlab'] })
    void navigate({ to: '/lab', search: { tab: 'trainings', session: event.session } })
  })
  useRobotEvent('robotlab.session', ({ session }) => {
    client.setQueryData(['robotlab', 'session', session.id], { session })
    void client.invalidateQueries({ queryKey: ['robotlab', 'sessions'] })
    void client.invalidateQueries({ queryKey: ['robotlab', 'environments'] })
    void client.invalidateQueries({ queryKey: ['robotlab', 'skills', session.id] })
  })
  return null
}

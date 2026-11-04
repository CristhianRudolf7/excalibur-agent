import { useNavigate, useSearch } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { MessageSquare } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { PanelToggle } from '@/components/ui/panel-toggle'
import { useShell } from '@/features/shell/store'
import { useAgents } from '@/features/agents/store'
import { CallOverlay } from '@/features/chat/voice/CallOverlay'
import { labCall, robotlabEnabled } from './api'
import { Environments, EnvironmentDetail } from './Environments'
import { InstallCard } from './InstallCard'
import { Trainings } from './Trainings'
import { LabLayout } from './LabLayout'
import { Tabs } from './Tabs'
import { SessionView } from './SessionView'
import { useLabText } from './i18n'
import { useLab } from './state'
import './robotlab.css'

export default function RobotLab({ mobile = false }: { mobile?: boolean }) {
  const { text } = useLabText()
  const shell = useShell()
  const agentOpen = useLab((state) => state.agentOpen)
  const navigate = useNavigate()
  const search = useSearch({ strict: false }) as { tab?: string; env?: string; session?: string }
  const tab = search.tab === 'trainings' ? 'trainings' : 'environments'
  const status = useQuery({
    queryKey: ['robotlab', 'status'],
    queryFn: () => labCall('robotlab.status', {}),
    enabled: robotlabEnabled,
    retry: false,
  })
  const draft = (value: string) => {
    useLab.getState().openAgent()
    if (value) useAgents.getState().prefill('excalibur', value)
  }
  const route = (next: { tab: string; env?: string; session?: string }) =>
    void navigate({ to: '/lab', search: next })
  return (
    <div className="robot-lab">
      <header className="chat-header lab-header">
        <PanelToggle
          side="left"
          label="sidebar"
          open={mobile ? shell.drawer : !shell.collapsed}
          onClick={() => shell.toggleSidebar(mobile)}
        />
        <h1>{text('title')}</h1>
        <Button
          variant="icon"
          className="icon-button"
          aria-label={text('agent')}
          aria-expanded={agentOpen}
          onClick={() => useLab.getState().openAgent()}
        >
          <MessageSquare />
        </Button>
      </header>
      {!robotlabEnabled || status.data?.installed === false ? (
        <InstallCard />
      ) : status.isPending ? (
        <p role="status">{text('loading')}</p>
      ) : status.error ? (
        <p role="alert">
          {status.error.message}
          <Button onClick={() => void status.refetch()}>{text('retry')}</Button>
        </p>
      ) : tab === 'trainings' && search.session ? (
        <SessionView
          id={search.session}
          mobile={mobile}
          onBack={() => route({ tab: 'trainings' })}
        />
      ) : (
        <LabLayout mobile={mobile}>
          <Tabs
            label={text('title')}
            values={['environments', 'trainings']}
            value={tab}
            onChange={(value) => route({ tab: value })}
          />
          <section className="lab-content">
            {tab === 'environments' ? (
              search.env ? (
                <EnvironmentDetail
                  key={search.env}
                  id={search.env}
                  onBack={() => route({ tab })}
                  onDraft={draft}
                />
              ) : (
                <Environments onSelect={(env) => route({ tab, env })} onDraft={draft} />
              )
            ) : (
              <Trainings
                onSelect={(session) => {
                  useLab.getState().openAgent()
                  route({ tab: 'trainings', session })
                }}
                onCreateEnvironment={() => {
                  route({ tab: 'environments' })
                  draft(text('createDraft'))
                }}
                onAgent={() => useLab.getState().openAgent()}
              />
            )}
          </section>
        </LabLayout>
      )}
      <CallOverlay />
    </div>
  )
}

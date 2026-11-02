import { useRouter } from '@tanstack/react-router'
import { useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { ArrowLeft, Clock3 } from 'lucide-react'
import type { Automation } from '@/lib/rpc/contract'
import { useAutomationDetail, useAutomationRuns } from './data'
import { formatDate, formatSchedule } from './format'
import { FlowDiagram } from './FlowDiagram'
import { DetailPanel } from './DetailPanel'
import './i18n'

function Skeleton() {
  return (
    <div className="auto-detail auto-skeleton" aria-busy="true">
      <div className="auto-skeleton-header">
        <i />
        <i />
        <i />
      </div>
      <div className="auto-detail-grid">
        <div className="auto-flow-card">
          <i />
          <i />
          <i />
        </div>
        <div className="auto-panel">
          <i />
          <i />
          <i />
        </div>
      </div>
    </div>
  )
}
export function AutomationDetailPage({
  id,
  actions,
}: {
  id: string
  actions?: (a: Automation) => ReactNode
}) {
  // Key the local replay/selection state when navigation changes the automation.
  return <AutomationDetail key={id} id={id} actions={actions} />
}
function AutomationDetail({ id, actions }: { id: string; actions?: (a: Automation) => ReactNode }) {
  const { t, i18n } = useTranslation('automationDetail')
  const detail = useAutomationDetail(id)
  const history = useAutomationRuns(id)
  const [selected, setSelected] = useState<string>()
  const [selectedRun, setSelectedRun] = useState<string>()
  const [tab, setTab] = useState('step')
  if (detail.isPending) return <Skeleton />
  if (detail.isError) {
    const missing = detail.error.message.toLowerCase().includes('not found')
    return (
      <div className="auto-detail auto-empty" role="alert">
        <Clock3 size={32} />
        <h1>{t(missing ? 'notFound' : 'loadError')}</h1>
        <a href="/automations">{t('back')}</a>
        {!missing && (
          <button type="button" className="auto-secondary" onClick={() => void detail.refetch()}>
            {t('retry')}
          </button>
        )}
      </div>
    )
  }
  const { automation, versions } = detail.data
  const schedule = formatSchedule(automation.schedule, t, i18n.language)
  const runs = history.data?.runs ?? (automation.last_run ? [automation.last_run] : [])
  const live = runs.find((run) => run.status === 'running')
  const run = runs.find((item) => item.id === selectedRun) ?? live ?? runs[0]
  const reason = automation.paused_reason
  const reasonText =
    reason === 'user'
      ? t('user')
      : reason === 'agent'
        ? t('agentReason')
        : reason === '3 consecutive failures'
          ? t('failures')
          : reason
  return (
    <main className="auto-detail">
      <BackLink label={t('back')} />
      <header className="auto-detail-header">
        <div className="auto-heading">
          <p className="auto-eyebrow">{t('eyebrow')}</p>
          <div className="auto-title-row">
            <h1>{automation.name}</h1>
            <span className={`auto-status auto-status-${live ? 'running' : automation.status}`}>
              <i />
              {t(live ? 'running' : automation.status)}
              {automation.status === 'paused' && reasonText && <span> · {reasonText}</span>}
            </span>
          </div>
          <p className="auto-description">{automation.description}</p>
          <div className="auto-schedule">
            <Clock3 size={15} />
            <span>{schedule}</span>
            {automation.next_run_at && (
              <span>
                {t('next', {
                  date: formatDate(
                    automation.next_run_at,
                    i18n.language,
                    automation.schedule.timezone,
                  ),
                })}
              </span>
            )}
            <span className="auto-version-tag">
              {t('version', { version: automation.version })}
            </span>
          </div>
        </div>
        {actions && <div className="auto-header-actions">{actions(automation)}</div>}
      </header>
      <div className="auto-detail-grid">
        <FlowDiagram
          automation={automation}
          schedule={schedule}
          run={run}
          selected={selected}
          onSelect={(step) => {
            setSelected(step)
            setTab('step')
          }}
        />
        <DetailPanel
          automation={automation}
          versions={versions}
          runs={runs}
          run={run}
          selected={selected}
          onRun={setSelectedRun}
          tab={tab}
          onTab={setTab}
          runsError={history.isError}
          retryRuns={() => void history.refetch()}
        />
      </div>
    </main>
  )
}

/** Back pill: client-side navigation inside the router, a plain link outside it. */
function BackLink({ label }: { label: string }) {
  const router = useRouter({ warn: false })
  return (
    <a
      className="auto-back"
      href="/automations"
      onClick={(event) => {
        if (!router || event.metaKey || event.ctrlKey || event.shiftKey || event.button !== 0)
          return
        event.preventDefault()
        void router.navigate({ to: '/automations' })
      }}
    >
      <ArrowLeft size={15} aria-hidden="true" />
      {label}
    </a>
  )
}

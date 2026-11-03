import { useAgents } from '@/features/agents/store'
import { useEffect, useState } from 'react'
import { Link, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Skeleton } from '@/components/shared'
import {
  CalendarClock,
  CalendarDays,
  CalendarRange,
  Check,
  Clock3,
  Repeat,
  Sunrise,
  Timer,
  Workflow,
  X,
} from 'lucide-react'
import type { AutomationSchedule } from '@/lib/rpc/contract'
import { useAutomations, describeSchedule, relativeTime } from './api'
import { AutomationActions } from './AutomationActions'
import { useAutomationContext } from './context'
import '@/styles/redesign/automations.css'
export function AutomationsPage() {
  const { t, i18n } = useTranslation()
  const query = useAutomations()
  const navigate = useNavigate()
  const [filter, setFilter] = useState('all')
  const [, tick] = useState(0)
  useEffect(() => {
    const timer = setInterval(() => tick((v) => v + 1), 60000)
    return () => clearInterval(timer)
  }, [])
  const items = query.data?.automations ?? []
  const filtered = items.filter((a) => filter === 'all' || a.status === filter)
  return (
    <section className="automations-page">
      <header className="auto-header">
        <div>
          <span className="auto-eyebrow">EXCALIBUR</span>
          <h1>{t('automations')}</h1>
          <p>{t('autoSubtitle')}</p>
        </div>
        <span className="auto-summary">
          {t('autoSummary', {
            active: items.filter((a) => a.status === 'active').length,
            paused: items.filter((a) => a.status === 'paused').length,
          })}
        </span>
      </header>
      <div className="auto-filters" role="group" aria-label={t('automations')}>
        <span
          className="auto-filter-indicator"
          aria-hidden="true"
          style={{ transform: `translateX(${['all', 'active', 'paused'].indexOf(filter) * 100}%)` }}
        />
        {[
          ['all', 'autoAll'],
          ['active', 'autoActiveFilter'],
          ['paused', 'autoPausedFilter'],
        ].map(([value, key]) => (
          <button key={value} aria-pressed={filter === value} onClick={() => setFilter(value!)}>
            {t(key!)}
          </button>
        ))}
      </div>
      {query.isPending ? (
        <div className="auto-grid" aria-busy="true">
          {[0, 1, 2].map((i) => (
            <div key={i} className="auto-card auto-skeleton">
              <Skeleton />
              <Skeleton />
              <Skeleton />
              <Skeleton />
            </div>
          ))}
        </div>
      ) : query.isError ? (
        <div role="alert" className="auto-empty">
          <p>{t('autoError')}</p>
          <button className="auto-button" onClick={() => void query.refetch()}>
            {t('autoRetry')}
          </button>
        </div>
      ) : items.length === 0 ? (
        <div className="auto-empty">
          <span className="auto-empty-icon">
            <Workflow size={36} />
          </span>
          <h2>{t('autoEmpty')}</h2>
          <p>{t('autoEmptyHint')}</p>
          <button
            className="auto-primary"
            onClick={() => {
              useAutomationContext.getState().prefill(t('autoExample'))
              void navigate({
                to: '/agents/$agentId',
                params: { agentId: useAgents.getState().favorite_id },
                search: { view: 'chat', panel: 'files' },
              })
            }}
          >
            {t('autoChat')}
          </button>
        </div>
      ) : (
        <div className="auto-grid">
          {filtered.map((a, index) => {
            const running = a.last_run?.status === 'running'
            return (
              <article
                className="auto-card"
                key={a.id}
                style={{ animationDelay: `${Math.min(index, 7) * 35}ms` }}
              >
                <Link
                  className="auto-card-link"
                  to="/automations/$id"
                  params={{ id: a.id }}
                  aria-label={a.name}
                  title={
                    a.status === 'paused'
                      ? a.consecutive_failures >= 3
                        ? t('autoFailedPause')
                        : a.paused_reason
                      : undefined
                  }
                >
                  <span className="sr-only">{a.name}</span>
                </Link>
                <div className="auto-card-top">
                  <span className="auto-mark" data-kind={a.schedule.kind} aria-hidden="true">
                    <ScheduleIcon kind={a.schedule.kind} />
                  </span>
                  <span
                    className={`auto-status ${running || a.status === 'active' ? 'auto-live' : ''}`}
                    title={a.consecutive_failures >= 3 ? t('autoFailedPause') : a.paused_reason}
                  >
                    <i />
                    {t(
                      running ? 'autoRunning' : a.status === 'active' ? 'autoActive' : 'autoPaused',
                    )}
                  </span>
                </div>
                <h2>{a.name}</h2>
                <p className="auto-description">{a.description}</p>
                <p className="auto-schedule">
                  <Clock3 size={16} />
                  {describeSchedule(a.schedule, t, i18n.language)}
                </p>
                <div className="auto-flow" aria-label={a.steps.map((s) => s.title).join(' → ')}>
                  {a.steps.map((s) => (
                    <span
                      key={s.id}
                      className={`auto-step auto-step-${s.type}`}
                      title={`${s.type}: ${s.title}`}
                    />
                  ))}
                </div>
                <div className="auto-timing">
                  <span>
                    {a.next_run_at
                      ? t('autoNext', { time: relativeTime(a.next_run_at, i18n.language) })
                      : t('autoNoNext')}
                  </span>
                  {a.last_run && !running && (
                    <span>
                      {a.last_run.status === 'succeeded' ? <Check size={14} /> : <X size={14} />}{' '}
                      {t(
                        a.last_run.status === 'succeeded' ? 'autoLastSucceeded' : 'autoLastFailed',
                        {
                          time: relativeTime(
                            a.last_run.finished_at ?? a.last_run.started_at,
                            i18n.language,
                          ),
                        },
                      )}
                    </span>
                  )}
                </div>
                <AutomationActions automation={a} compact />
              </article>
            )
          })}
        </div>
      )}
    </section>
  )
}

const scheduleIcons = {
  once: CalendarClock,
  interval: Repeat,
  daily: Sunrise,
  weekly: CalendarDays,
  monthly: CalendarRange,
  cron: Timer,
} satisfies Record<AutomationSchedule['kind'], unknown>

function ScheduleIcon({ kind }: { kind: AutomationSchedule['kind'] }) {
  const Icon = scheduleIcons[kind] ?? CalendarClock
  return <Icon size={20} />
}

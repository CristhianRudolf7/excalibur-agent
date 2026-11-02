import { useEffect, useId, useState, type ReactNode } from 'react'
import { useTranslation } from 'react-i18next'
import { CheckCircle2, Circle, CircleAlert, Clock3 } from 'lucide-react'
import type { Automation, AutomationRun, AutomationVersion } from '@/lib/rpc/contract'
import { formatDate, formatSchedule, relativeTime } from './format'
import { StepIcon } from './FlowDiagram'
import './i18n'

export function DetailPanel({
  automation,
  versions,
  runs,
  run,
  selected,
  onRun,
  tab,
  onTab,
  runsError,
  retryRuns,
}: {
  automation: Automation
  versions: AutomationVersion[]
  runs: AutomationRun[]
  run?: AutomationRun
  selected?: string
  onRun: (id: string) => void
  tab: string
  onTab: (tab: string) => void
  runsError: boolean
  retryRuns: () => void
}) {
  const { t, i18n } = useTranslation('automationDetail')
  const uid = useId()
  const [now, setNow] = useState(Date.now)
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 30_000)
    return () => window.clearInterval(timer)
  }, [])
  const step = automation.steps.find((item) => item.id === selected)
  const output =
    run?.version === automation.version
      ? run.steps.find((item) => item.step_id === selected)
      : undefined
  const tabs = ['step', 'runs', 'versions'] as const
  const field = (label: string, content: ReactNode) => (
    <section className="auto-detail-field">
      <h3>{label}</h3>
      {content}
    </section>
  )
  return (
    <aside className="auto-panel">
      <div className="auto-tabs" role="tablist" aria-label={t('eyebrow')}>
        {tabs.map((item, index) => (
          <button
            type="button"
            role="tab"
            key={item}
            id={`${uid}-${item}`}
            aria-controls={`${uid}-content`}
            aria-selected={tab === item}
            tabIndex={tab === item ? 0 : -1}
            onClick={() => onTab(item)}
            onKeyDown={(event) => {
              if (!['ArrowRight', 'ArrowLeft', 'Home', 'End'].includes(event.key)) return
              event.preventDefault()
              const next =
                event.key === 'Home'
                  ? 0
                  : event.key === 'End'
                    ? 2
                    : (index + (event.key === 'ArrowRight' ? 1 : 2)) % 3
              const nextTab = tabs[next]!
              onTab(nextTab)
              document.getElementById(`${uid}-${nextTab}`)?.focus()
            }}
          >
            {t(item)}
          </button>
        ))}
      </div>
      <div
        className="auto-panel-content"
        id={`${uid}-content`}
        role="tabpanel"
        aria-labelledby={`${uid}-${tab}`}
      >
        {run && run.version !== automation.version && (
          <p className="auto-version-warning">{t('oldVersion', { version: run.version })}</p>
        )}
        {tab === 'step' &&
          (selected === '__schedule' ? (
            <>
              <div className="auto-panel-title">
                <StepIcon type="schedule" />
                <h2>{t('schedule')}</h2>
              </div>
              <p>{formatSchedule(automation.schedule, t, i18n.language)}</p>
            </>
          ) : step ? (
            <>
              <div className="auto-panel-title">
                <span className={`auto-type-icon auto-type-${step.type}`}>
                  <StepIcon type={step.type} />
                </span>
                <div>
                  <small>{t(step.type)}</small>
                  <h2>{step.title}</h2>
                </div>
              </div>
              {step.instruction && field(t('instruction'), <p>{step.instruction}</p>)}
              {step.code &&
                field(
                  t('code'),
                  <pre className="auto-code" tabIndex={0}>
                    <code>{step.code}</code>
                  </pre>,
                )}
              {step.tool && field(t('tool'), <code>{step.tool}</code>)}
              {step.args &&
                field(t('args'), <pre tabIndex={0}>{JSON.stringify(step.args, null, 2)}</pre>)}
              {step.expression && field(t('expression'), <pre tabIndex={0}>{step.expression}</pre>)}
              {step.message && field(t('message'), <p className="auto-template">{step.message}</p>)}
              {output && (
                <p className={`auto-run-state auto-text-${output.status}`}>{t(output.status)}</p>
              )}
              {output?.output_preview &&
                field(t('output'), <pre tabIndex={0}>{output.output_preview}</pre>)}
              {output?.error &&
                field(t('error'), <p className="auto-error-text">{output.error}</p>)}
            </>
          ) : (
            <p className="auto-panel-empty">{t('select')}</p>
          ))}
        {tab === 'runs' && (
          <>
            {runsError && (
              <div role="alert">
                <p>{t('runsError')}</p>
                <button type="button" className="auto-secondary" onClick={retryRuns}>
                  {t('retry')}
                </button>
              </div>
            )}
            {!runs.length && !runsError && <p className="auto-panel-empty">{t('noRuns')}</p>}
            <ol className="auto-run-list">
              {[...runs]
                .sort(
                  (a, b) =>
                    Number(b.status === 'running') - Number(a.status === 'running') ||
                    b.started_at.localeCompare(a.started_at),
                )
                .map((item) => {
                  const Icon =
                    item.status === 'succeeded'
                      ? CheckCircle2
                      : item.status === 'failed'
                        ? CircleAlert
                        : item.status === 'running'
                          ? Clock3
                          : Circle
                  return (
                    <li key={item.id}>
                      <button
                        type="button"
                        className={`auto-run-row ${run?.id === item.id ? 'auto-run-selected' : ''}`}
                        onClick={() => onRun(item.id)}
                        aria-pressed={run?.id === item.id}
                      >
                        <span className="auto-run-top">
                          <Icon
                            size={17}
                            className={`auto-text-${item.status}`}
                            aria-label={t(item.status)}
                          />
                          <strong>
                            {t(item.trigger === 'schedule' ? 'scheduleTrigger' : item.trigger)}
                          </strong>
                          {item.status === 'running' && (
                            <span className="auto-live">{t('live')}</span>
                          )}
                        </span>
                        <span className="auto-run-meta">
                          <time
                            dateTime={item.started_at}
                            title={formatDate(item.started_at, i18n.language)}
                          >
                            {relativeTime(item.started_at, i18n.language, now)}
                          </time>
                          <span>
                            {t('duration', {
                              seconds: Math.max(
                                0,
                                Math.round(
                                  ((item.finished_at ? new Date(item.finished_at).getTime() : now) -
                                    new Date(item.started_at).getTime()) /
                                    1000,
                                ),
                              ),
                            })}
                          </span>
                        </span>
                        <span>{item.summary ?? item.error ?? t('noSummary')}</span>
                      </button>
                    </li>
                  )
                })}
            </ol>
          </>
        )}
        {tab === 'versions' && (
          <>
            <ol className="auto-version-list">
              {[...versions]
                .sort((a, b) => b.version - a.version)
                .map((version) => (
                  <li key={version.version}>
                    <div>
                      <strong>{t('version', { version: version.version })}</strong>
                      {version.version === automation.version && (
                        <span className="auto-live">{t('current')}</span>
                      )}
                    </div>
                    <time dateTime={version.created_at}>
                      {formatDate(version.created_at, i18n.language)}
                    </time>
                    {version.note && <p>{version.note}</p>}
                  </li>
                ))}
            </ol>
            <p className="auto-version-hint">{t('versionHint')}</p>
          </>
        )}
      </div>
    </aside>
  )
}

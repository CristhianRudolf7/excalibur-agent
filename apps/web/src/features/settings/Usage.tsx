import { loadingLazy as lazy, LoadingFallback } from '@/features/shell/loading'
import { useLoading } from '@/components/shared'
import { ChartSkeleton, UsageSkeleton } from './Skeletons'
import { Suspense, useEffect, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { rpc } from '@/lib/rpc/client'
import { Segmented } from './Segmented'
import { Feedback } from './feedback'
const UsageChart = lazy(() => import('./UsageChart'))
function Count({ value, currency = false }: { value: number; currency?: boolean }) {
  const { i18n } = useTranslation()
  const [shown, setShown] = useState(value)
  const reducedMotion = matchMedia('(prefers-reduced-motion: reduce)').matches
  useEffect(() => {
    if (reducedMotion) return
    let frame = 0
    const start = performance.now()
    const tick = (now: number) => {
      const fraction = Math.min(1, (now - start) / 420)
      setShown(value * (1 - (1 - fraction) ** 3))
      if (fraction < 1) frame = requestAnimationFrame(tick)
    }
    frame = requestAnimationFrame(tick)
    return () => cancelAnimationFrame(frame)
  }, [value, reducedMotion])
  const formatter = new Intl.NumberFormat(
    i18n.language,
    currency
      ? { style: 'currency', currency: 'USD', maximumFractionDigits: 4 }
      : { maximumFractionDigits: 0 },
  )
  const finalValue = reducedMotion ? value : shown
  return (
    <strong aria-label={formatter.format(value)}>
      <span aria-hidden="true">
        {formatter
          .formatToParts(finalValue)
          .map((part, index) =>
            part.type === 'currency' ? <small key={index}>{part.value}</small> : part.value,
          )}
      </span>
    </strong>
  )
}
export default function Usage() {
  const { t, i18n } = useTranslation()
  const [range, setRange] = useState(7)
  const [until] = useState(() => new Date().toISOString())
  const query = useQuery({
    queryKey: ['usage', range, until],
    queryFn: ({ signal }) =>
      rpc.call(
        'usage.summary',
        {
          since: new Date(new Date(until).getTime() - range * 86400_000).toISOString(),
          until,
          bucket: range === 1 ? 'hour' : 'day',
        },
        { signal },
      ),
    retry: false,
  })
  const loading = useLoading(query.isPending)
  const data = loading.busy ? undefined : query.data
  return (
    <>
      <div className="settings-toolbar usage-toolbar">
        <h2 className="settings-eyebrow">{t('settingsUsage')}</h2>
        <Segmented
          label={t('settingsRange')}
          value={range}
          onChange={setRange}
          options={[1, 7, 30].map((value) => ({
            value,
            label: t(value === 1 ? 'settings24Short' : value === 7 ? 'settings7' : 'settings30'),
          }))}
        />
      </div>
      <Feedback loading={loading} query={query} skeleton={<UsageSkeleton />} />
      {data && (
        <>
          <div className="usage-kpis">
            {(
              [
                ['input_tokens', 'settingsInput'],
                ['cached_tokens', 'settingsCached'],
                ['output_tokens', 'settingsOutput'],
                ['cost_usd', 'settingsCost'],
                ['tool_calls', 'settingsCalls'],
              ] as const
            ).map(([key, label]) => (
              <article
                className={`settings-card usage-kpi ${key === 'cost_usd' ? 'usage-cost' : ''}`}
                key={key}
              >
                <span>{t(label)}</span>
                <Count value={data.totals[key]} currency={key === 'cost_usd'} />
                <Sparkline values={data.by_bucket.map((bucket) => bucket[key])} />
                {!data.totals[key] && (
                  <p className="usage-no-activity">{t('settingsNoActivity')}</p>
                )}
              </article>
            ))}
          </div>
          <article className="settings-card">
            <h3>{t('settingsTimeline')}</h3>
            {data.by_bucket.length ? (
              <>
                <Suspense
                  fallback={
                    <LoadingFallback>
                      <ChartSkeleton />
                    </LoadingFallback>
                  }
                >
                  <UsageChart buckets={data.by_bucket} />
                </Suspense>
                <details>
                  <summary>{t('settingsTimeline')}</summary>
                  <div className="settings-table">
                    <table>
                      <thead>
                        <tr>
                          {[
                            'settingsStarted',
                            'settingsInput',
                            'settingsCached',
                            'settingsOutput',
                            'settingsCost',
                            'settingsCalls',
                          ].map((key) => (
                            <th key={key}>{t(key)}</th>
                          ))}
                        </tr>
                      </thead>
                      <tbody>
                        {data.by_bucket.map((row) => (
                          <tr key={row.start}>
                            <td>{new Date(row.start).toLocaleString(i18n.language)}</td>
                            <td>{row.input_tokens}</td>
                            <td>{row.cached_tokens}</td>
                            <td>{row.output_tokens}</td>
                            <td>{row.cost_usd}</td>
                            <td>{row.tool_calls}</td>
                          </tr>
                        ))}
                      </tbody>
                    </table>
                  </div>
                </details>
              </>
            ) : (
              <EmptyChart />
            )}
          </article>
          <div className="usage-breakdowns">
            <article className="settings-card">
              <h3>{t('settingsModels')}</h3>
              {data.by_model.length ? (
                <>
                  <Suspense
                    fallback={
                      <LoadingFallback>
                        <ChartSkeleton />
                      </LoadingFallback>
                    }
                  >
                    <UsageChart models={data.by_model} />
                  </Suspense>
                  {data.by_model.map((row, index) => (
                    <div className="usage-breakdown usage-model" key={row.model}>
                      <i
                        className={`usage-model-dot usage-model-dot-${index % 5}`}
                        aria-hidden="true"
                      />
                      <div>
                        <strong>{row.model}</strong>
                        <span>
                          {t('settingsInput')}: {row.input_tokens.toLocaleString(i18n.language)} ·{' '}
                          {t('settingsOutput')}: {row.output_tokens.toLocaleString(i18n.language)}
                        </span>
                      </div>
                      <Count value={row.cost_usd} currency />
                    </div>
                  ))}
                </>
              ) : (
                <EmptyChart kind="donut" />
              )}
            </article>
            <article className="settings-card">
              <h3>{t('settingsTools')}</h3>
              {data.by_tool.length ? (
                data.by_tool.map((row) => (
                  <div className="usage-breakdown" key={row.tool}>
                    <strong>{row.tool}</strong>
                    <div className="usage-tool-track" aria-hidden="true">
                      <i
                        style={{
                          transform: `scaleX(${row.calls / Math.max(1, ...data.by_tool.map((tool) => tool.calls))})`,
                        }}
                      />
                    </div>
                    <span>
                      {t('settingsCalls')}: {row.calls} · {t('settingsFailures')}: {row.failures}
                    </span>
                  </div>
                ))
              ) : (
                <EmptyChart kind="bars" />
              )}
            </article>
          </div>
          <article className="settings-card">
            <h3>{t('settingsRuns')}</h3>
            {data.recent_runs.length ? (
              <div className="settings-table">
                <table>
                  <thead>
                    <tr>
                      {[
                        'settingsRun',
                        'settingsStarted',
                        'settingsDuration',
                        'settingsInput',
                        'settingsOutput',
                        'settingsCost',
                        'settingsCalls',
                        'settingsStatus',
                      ].map((key) => (
                        <th key={key}>{t(key)}</th>
                      ))}
                    </tr>
                  </thead>
                  <tbody>
                    {data.recent_runs.map((row) => (
                      <tr key={row.run_id}>
                        <td>{row.task_short_code ?? row.run_id}</td>
                        <td>{new Date(row.started_at).toLocaleString(i18n.language)}</td>
                        <td>{row.duration_ms}</td>
                        <td>{row.input_tokens}</td>
                        <td>{row.output_tokens}</td>
                        <td>
                          <Count value={row.cost_usd} currency />
                        </td>
                        <td>{row.tool_calls}</td>
                        <td>{row.status}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            ) : (
              <EmptyChart kind="table" />
            )}
          </article>
        </>
      )}
    </>
  )
}

function Sparkline({ values }: { values: number[] }) {
  if (!values.length || !values.some(Boolean))
    return <div className="usage-sparkline-flat" aria-hidden="true" />
  const max = Math.max(...values, 1)
  const points = values
    .map(
      (value, index) =>
        `${(index / Math.max(1, values.length - 1)) * 120},${30 - (value / max) * 25}`,
    )
    .join(' ')
  return (
    <svg className="usage-sparkline" viewBox="0 0 120 34" aria-hidden="true">
      <polyline points={values.length === 1 ? `0,5 120,5` : points} />
    </svg>
  )
}
function EmptyChart({ kind = 'timeline' }: { kind?: string }) {
  const { t } = useTranslation()
  return (
    <div className={`usage-empty usage-empty-${kind}`}>
      <div className="usage-ghost" aria-hidden="true">
        {[35, 60, 45, 80, 55, 95, 70, 50, 85, 65, 90, 75].map((height, index) => (
          <i key={index} style={{ height: `${height}%` }} />
        ))}
      </div>
      <strong>{t('settingsEmpty')}</strong>
      <p>{t('settingsEmptyNote')}</p>
    </div>
  )
}

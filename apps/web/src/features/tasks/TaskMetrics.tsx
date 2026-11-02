import { useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import type { TaskStatus } from '@/lib/rpc/contract'
import { TaskStatusIcon } from './TaskStatusIcon'

function CountUp({ value }: { value: number }) {
  const [display, setDisplay] = useState(() =>
    matchMedia('(prefers-reduced-motion: reduce)').matches ? value : 0,
  )
  const current = useRef(display)
  useEffect(() => {
    const motion = matchMedia('(prefers-reduced-motion: reduce)')
    let frame = 0
    const from = current.current
    const start = performance.now()
    const finish = () => {
      cancelAnimationFrame(frame)
      current.current = value
      setDisplay(value)
    }
    const tick = (now: number) => {
      const progress = Math.min((now - start) / 420, 1)
      current.current = Math.round(from + (value - from) * (1 - (1 - progress) ** 3))
      setDisplay(current.current)
      if (progress < 1) frame = requestAnimationFrame(tick)
    }
    if (motion.matches) finish()
    else frame = requestAnimationFrame(tick)
    motion.addEventListener('change', finish)
    return () => {
      cancelAnimationFrame(frame)
      motion.removeEventListener('change', finish)
    }
  }, [value])
  return <span aria-hidden="true">{display}</span>
}

export function TaskMetrics({ counts }: { counts?: Record<TaskStatus, number> }) {
  const { t } = useTranslation()
  return (
    <div className="work-metrics">
      {(['pending', 'in_progress', 'blocked', 'done'] as const).map((status) => (
        <section
          key={status}
          className={`work-metric ${status === 'done' ? 'work-metric-ink' : ''}`}
        >
          <div className="work-metric-label">
            <TaskStatusIcon status={status} />
            {t(`work_${status}`)}
          </div>
          <strong aria-label={String(counts?.[status] ?? '—')}>
            {counts ? <CountUp value={counts[status]} /> : '—'}
          </strong>
          <span className="work-metric-caption">{t('workTasksCaption')}</span>
          <span className="work-metric-line" aria-hidden="true" />
        </section>
      ))}
    </div>
  )
}

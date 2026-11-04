import { useEffect, useLayoutEffect, useMemo, useRef, useState } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import {
  Bot,
  Check,
  ChevronRight,
  CircleCheck,
  CircleX,
  Clock3,
  FileDiff,
  FileSearch,
  ListChecks,
  LoaderCircle,
  Terminal,
  Wrench,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { CodingEntry, CodingRun } from './coding-store'
import { activeCodingStates } from './coding-timeline'
import type { Message } from './types'
import { testSummary } from './test-summary'
import './coding-activity.css'

const activeStates = activeCodingStates
const emptyNotices: Message[] = []
const stateKey = (state: string) =>
  activeStates.includes(state)
    ? state === 'queued'
      ? 'codingQueued'
      : 'codingRunning'
    : ['completed', 'done'].includes(state)
      ? 'codingDone'
      : ['paused', 'blocked', 'waiting'].includes(state)
        ? 'codingWaiting'
        : ['canceled', 'cancelled'].includes(state)
          ? 'codingCanceled'
          : 'codingFailed'
function StateIcon({ state }: { state: string }) {
  const Icon = activeStates.includes(state)
    ? LoaderCircle
    : ['completed', 'done'].includes(state)
      ? CircleCheck
      : ['paused', 'blocked', 'waiting'].includes(state)
        ? Clock3
        : CircleX
  return (
    <Icon
      size={14}
      aria-hidden="true"
      className={activeStates.includes(state) ? 'coding-spinner' : undefined}
    />
  )
}
function RunHeader({
  run,
  open,
  toggle,
  now,
}: {
  run: CodingRun
  open: boolean
  toggle: () => void
  now: number
}) {
  const { t } = useTranslation()
  const seconds = Math.max(
    0,
    Math.floor(
      (activeStates.includes(run.state)
        ? now - Date.parse(run.started)
        : (run.elapsed ?? Date.parse(run.updated) - Date.parse(run.started))) / 1000,
    ),
  )
  return (
    <div className="coding-run-header">
      <div className="coding-run-title">
        <Bot size={15} aria-hidden="true" />
        <strong>{run.name || t(run.parent ? 'codingSubagent' : 'codingAssistant')}</strong>
        <span className={`coding-state ${stateKey(run.state)}`}>
          <StateIcon state={run.state} />
          {t(stateKey(run.state))}
        </span>
        <time>{t('codingSeconds', { count: seconds })}</time>
      </div>
      {run.steps.length > 0 && (
        <>
          <button
            className="coding-plan-toggle"
            type="button"
            onClick={toggle}
            aria-expanded={open}
          >
            <ChevronRight size={12} className={open ? 'coding-chevron-open' : undefined} />
            <ListChecks size={13} aria-hidden="true" />
            {t('codingSteps', { done: run.done, total: run.steps.length })}
          </button>
          {open && (
            <ol className="coding-plan">
              {run.steps.map((step, index) => (
                <li key={`${index}:${step}`}>
                  <Check
                    size={12}
                    aria-hidden="true"
                    className={index < run.done ? 'coding-passed' : 'coding-pending'}
                  />
                  {step}
                </li>
              ))}
            </ol>
          )}
        </>
      )}
      {run.omitted > 0 && <small>{t('codingEarlier', { count: run.omitted })}</small>}
    </div>
  )
}
function ActivityEntry({
  entry,
  open,
  toggle,
}: {
  entry: CodingEntry
  open: boolean
  toggle: () => void
}) {
  const { t } = useTranslation()
  const tests = testSummary(entry.command ?? '', entry.output)
  const edit = entry.type === 'edit'
  const Icon = edit
    ? FileDiff
    : tests
      ? ListChecks
      : entry.kind === 'read'
        ? FileSearch
        : entry.kind === 'command'
          ? Terminal
          : Wrench
  const detail = edit ? entry.diff : entry.output
  const title = entry.command || entry.path || entry.tool || t('codingTool')
  const label = edit
    ? 'codingEdit'
    : tests
      ? 'codingTest'
      : entry.kind === 'read'
        ? 'codingRead'
        : entry.kind === 'command'
          ? 'codingCommand'
          : 'codingTool'
  const expandable = !!detail || entry.binary || entry.truncated
  const status = entry.status ?? 'completed'
  return (
    <div className="coding-entry" data-kind={edit ? 'edit' : tests ? 'test' : entry.kind}>
      <button
        type="button"
        className="coding-entry-toggle"
        aria-expanded={expandable ? open : undefined}
        aria-label={t('codingDetails', { kind: t(label), title })}
        onClick={toggle}
        disabled={!expandable}
      >
        <ChevronRight
          size={12}
          aria-hidden="true"
          className={open ? 'coding-chevron-open' : undefined}
        />
        <Icon size={15} aria-hidden="true" />
        <span className="coding-entry-label">{t(label)}</span>
        <code title={title}>{title}</code>
        <span className="coding-chips">
          {edit ? (
            <>
              <span className="coding-passed">+{entry.added ?? 0}</span>
              <span className="coding-failed">−{entry.removed ?? 0}</span>
            </>
          ) : (
            <>
              {tests?.passed !== undefined && (
                <span className="coding-passed">
                  {t(tests.unit === 'packages' ? 'codingPackagesPassed' : 'codingPassed', {
                    count: tests.passed,
                  })}
                </span>
              )}
              {tests?.failed !== undefined && (
                <span className={tests.failed ? 'coding-failed' : 'coding-muted'}>
                  {t('codingFailedCount', { count: tests.failed })}
                </span>
              )}
              {entry.exit_code !== undefined ? (
                <span className={entry.exit_code === 0 ? 'coding-passed' : 'coding-failed'}>
                  {t('codingExit', { code: entry.exit_code })}
                </span>
              ) : (
                <span className="coding-muted">
                  <StateIcon state={status} />
                  <span className="sr-only">{t(stateKey(status))}</span>
                </span>
              )}
            </>
          )}
        </span>
      </button>
      {!edit && entry.output && !open && (
        <pre className="coding-output-preview">
          {entry.output.trimEnd().split('\n').slice(-2).join('\n')}
        </pre>
      )}
      {open && (
        <div className="coding-details">
          {entry.binary ? (
            <p>{t('codingBinary')}</p>
          ) : edit ? (
            <pre className="coding-diff">
              {detail?.split('\n').map((line, index) => (
                <span
                  key={index}
                  className={
                    line.startsWith('+')
                      ? 'coding-diff-add'
                      : line.startsWith('-')
                        ? 'coding-diff-del'
                        : undefined
                  }
                >
                  {line}
                  {'\n'}
                </span>
              ))}
            </pre>
          ) : (
            <pre>{detail}</pre>
          )}
          {entry.truncated && <small>{t('codingTruncated')}</small>}
        </div>
      )}
    </div>
  )
}

/** Each turn retains its summary at the original position after completion. */
export function CodingActivity({
  runs,
  notices = emptyNotices,
}: {
  runs: CodingRun[]
  notices?: Message[]
}) {
  const { t } = useTranslation()
  const running = runs.some((run) => activeStates.includes(run.state))
  const [open, setOpen] = useState(false)
  const expanded = running || open
  const actions = runs.reduce((count, run) => count + run.entries.length + run.omitted, 0)
  const agents = runs.filter((run) => run.parent).length
  const summary = `${t('codingActions', { count: actions })} · ${t('codingAgents', { count: agents })}`
  if (!runs.length) return null
  return (
    <section className="coding-activity" aria-label={t('codingTitle')}>
      <button
        type="button"
        className="coding-header"
        aria-expanded={expanded}
        aria-label={`${t('codingTitle')} · ${summary} · ${t(running ? 'codingLive' : 'codingFinished')}`}
        onClick={() => setOpen(!open)}
        disabled={running}
      >
        <ChevronRight
          size={14}
          className={expanded ? 'coding-chevron-open' : undefined}
          aria-hidden="true"
        />
        <Terminal size={16} aria-hidden="true" />
        <strong>{t('codingTitle')}</strong>
        <span className="coding-summary">{summary}</span>
        <span className={running ? 'coding-live' : 'sr-only'} role="status">
          {t(running ? 'codingLive' : 'codingFinished')}
        </span>
      </button>
      {expanded && <ActivityTimeline runs={runs} notices={notices} running={running} />}
    </section>
  )
}

/** Measurements and output updates can change row heights after React commits.
 * Observe the virtual canvas as well as new rows to keep following the real end. */
function ActivityTimeline({
  runs,
  notices,
  running,
}: {
  runs: CodingRun[]
  notices: Message[]
  running: boolean
}) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState<Record<string, boolean>>({})
  const [now, setNow] = useState(Date.now)
  const scroll = useRef<HTMLDivElement>(null)
  const canvas = useRef<HTMLDivElement>(null)
  const following = useRef(true)
  const lastScrollTop = useRef(0)
  useEffect(() => {
    if (!running) return
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [running])
  const rows = useMemo(() => {
    const depth = (run: CodingRun, seen = new Set<string>()): number => {
      if (seen.has(run.id)) return 0
      seen.add(run.id)
      const parent = runs.find((item) => item.id === run.parent)
      return parent ? Math.min(depth(parent, seen) + 1, 3) : run.parent ? 1 : 0
    }
    // Keep the whole turn chronological, including interleaved nested agents.
    // Completion and session polls update their original rows in place.
    const events = runs
      .flatMap((run) => {
        const level = depth(run)
        return [
          {
            id: `run:${run.id}`,
            run,
            depth: level,
            entry: undefined as CodingEntry | undefined,
            at: run.started,
            sequence: run.sequence,
          },
          ...run.entries.map((entry) => ({
            id: `${run.id}:${entry.id}`,
            run,
            entry,
            depth: level,
            at: entry.at,
            sequence: entry.sequence,
          })),
        ]
      })
      .map((row) => ({ ...row, notice: undefined as Message | undefined }))
    return [
      ...events,
      ...notices.slice(-200).map((notice) => ({
        id: `notice:${notice.id}`,
        notice,
        run: undefined,
        entry: undefined,
        depth: 0,
        at: notice.at ?? runs[0]!.started,
        sequence: notice.sequence,
      })),
    ].sort((a, b) => Date.parse(a.at) - Date.parse(b.at) || (a.sequence ?? 0) - (b.sequence ?? 0))
  }, [runs, notices])
  // oxlint-disable-next-line react/incompatible-library -- Virtual rows use measured DOM heights.
  const virtual = useVirtualizer({
    count: rows.length,
    getItemKey: (index) => rows[index]!.id,
    getScrollElement: () => scroll.current,
    estimateSize: (index) => (rows[index]?.entry ? 52 : 80),
    overscan: 6,
    initialRect: { width: 600, height: 320 },
  })
  useLayoutEffect(() => {
    const node = scroll.current
    const content = canvas.current
    if (!node || !content) return
    const follow = () => {
      if (!following.current || !running) return
      node.scrollTop = node.scrollHeight
      lastScrollTop.current = node.scrollTop
    }
    follow()
    if (typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(follow)
    observer.observe(content)
    observer.observe(node)
    return () => observer.disconnect()
  }, [rows, running])
  const toggle = (id: string) => setExpanded((old) => ({ ...old, [id]: !old[id] }))
  return (
    <div
      className="coding-scroll"
      ref={scroll}
      onScroll={() => {
        const node = scroll.current
        if (!node) return
        const movedUp = node.scrollTop < lastScrollTop.current - 1
        lastScrollTop.current = node.scrollTop
        if (node.scrollHeight - node.scrollTop - node.clientHeight < 24) following.current = true
        else if (movedUp) following.current = false
      }}
      role="log"
      aria-live="off"
      aria-label={t('codingTimeline')}
      tabIndex={0}
    >
      <div ref={canvas} style={{ height: virtual.getTotalSize(), position: 'relative' }}>
        {virtual.getVirtualItems().map((item) => {
          const row = rows[item.index]!
          return (
            <div
              className="coding-virtual-row"
              data-index={item.index}
              data-parent-run={row.run?.parent}
              data-run={row.run?.id}
              key={row.id}
              ref={virtual.measureElement}
              style={{
                position: 'absolute',
                top: 0,
                left: 0,
                width: '100%',
                transform: `translateY(${item.start}px)`,
                paddingInlineStart: Math.min(row.depth, 2) * 12,
              }}
            >
              {row.notice ? (
                <p className="coding-notice">{row.notice.text}</p>
              ) : row.entry ? (
                <ActivityEntry
                  entry={row.entry}
                  open={!!expanded[row.id]}
                  toggle={() => toggle(row.id)}
                />
              ) : (
                <RunHeader
                  run={row.run!}
                  open={!!expanded[row.id]}
                  toggle={() => toggle(row.id)}
                  now={now}
                />
              )}
            </div>
          )
        })}
      </div>
    </div>
  )
}

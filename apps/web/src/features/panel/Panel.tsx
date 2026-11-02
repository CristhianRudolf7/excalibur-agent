import { useEffect, useMemo, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  Activity,
  ArrowUpRight,
  Bot,
  Check,
  FileDiff,
  FolderOpen,
  Layers,
  Workflow,
} from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { useChat } from '@/features/chat/store'
import { conversationId } from '@/features/chat/live'
import { useWhiteboard } from '@/features/whiteboard/store'
import type { PanelView } from '@/features/files/open'
import { rpc } from '@/lib/rpc/client'
import { subscribeEvents } from '@/lib/rpc/events'
import type { ActivityItem, FileChange } from '@/lib/rpc/contract'
import { ChangeList } from './ChangeList'
import { conversationChanges } from './changes'
const emptyMessages: never[] = []
const finished = (item: ActivityItem) => ['completed', 'failed', 'cancelled'].includes(item.state)
export function mergeActivity(items: ActivityItem[], item: ActivityItem) {
  const previous = items.find((entry) => entry.id === item.id)
  if (previous && Date.parse(previous.updated_at) > Date.parse(item.updated_at)) return items
  return [...items.filter((entry) => entry.id !== item.id), item]
    .sort(
      (a, b) =>
        Number(finished(a)) - Number(finished(b)) ||
        Date.parse(b.updated_at) - Date.parse(a.updated_at),
    )
    .slice(0, 100)
}
export function usePanelData(agent: string) {
  const client = useQueryClient()
  const messages = useChat((s) => s.live[agent]) ?? emptyMessages
  const changes = useMemo(() => conversationChanges(messages), [messages])
  const conversation = conversationId(agent)
  const key = useMemo(() => ['panel-activity', conversation], [conversation])
  const query = useQuery({
    queryKey: key,
    queryFn: async ({ signal }) => {
      const result = await rpc.call('activity.list', { conversation_id: conversation }, { signal })
      // Events received while the snapshot was loading take precedence.
      return {
        items: (client.getQueryData<{ items: ActivityItem[] }>(key)?.items ?? []).reduce(
          (items, item) => mergeActivity(items, item),
          result.items ?? [],
        ),
      }
    },
    retry: false,
  })
  const { refetch } = query
  useEffect(
    () =>
      subscribeEvents({
        types: ['activity.updated'],
        ready: () => {
          void refetch()
        },
        offline: () => {},
        event: (event) => {
          const payload = event.payload
          if (
            !('item' in payload) ||
            !payload.item ||
            ('conversation_id' in payload &&
              payload.conversation_id &&
              payload.conversation_id !== conversation)
          )
            return
          const item = payload.item
          // Unscoped bus events may belong to another conversation. Resolve new
          // IDs through the conversation-scoped list before displaying them.
          const known = client
            .getQueryData<{ items: ActivityItem[] }>(key)
            ?.items.some((entry) => entry.id === item.id)
          if (
            !known &&
            !('conversation_id' in payload && payload.conversation_id === conversation)
          ) {
            void refetch()
            return
          }
          client.setQueryData<{ items: ActivityItem[] }>(key, (old) => ({
            items: mergeActivity(old?.items ?? [], item),
          }))
        },
      }),
    [client, key, conversation, refetch],
  )
  return {
    changes,
    items: query.data?.items ?? [],
    loading: query.isPending,
    error: query.isError,
    retry: (): void => {
      void refetch()
    },
  }
}
type PanelData = ReturnType<typeof usePanelData>
export function PanelHome({
  data,
  onView,
}: {
  data: PanelData
  onView: (view: PanelView) => void
}) {
  const { t } = useTranslation()
  const board = useWhiteboard((s) => s.board)
  const cards = [
    {
      view: 'activity',
      icon: Activity,
      title: 'activity',
      hint: 'panelActivityHint',
      badge: t('panelRunning', { count: data.items.filter((item) => !finished(item)).length }),
    },
    {
      view: 'changes',
      icon: FileDiff,
      title: 'panelChangesTitle',
      hint: 'panelChangesHint',
      badge: `${data.changes.length} · +${data.changes.reduce((n, c) => n + c.added, 0)} −${data.changes.reduce((n, c) => n + c.removed, 0)}`,
    },
    { view: 'files', icon: FolderOpen, title: 'files', hint: 'panelFilesHint', badge: 'Excalibur' },
    {
      view: 'whiteboard',
      icon: Layers,
      title: 'whiteboard',
      hint: 'panelBoardHint',
      badge: board ? '1' : undefined,
    },
  ] as const
  return (
    <div className="panel-home">
      <p className="panel-eyebrow">{t('panelEyebrow')}</p>
      <h2>{t('panelTitle')}</h2>
      <p className="panel-subtitle">{t('panelSubtitle')}</p>
      <div className="panel-launchers">
        {cards.map((card, index) => (
          <button
            className="panel-launcher"
            key={card.view}
            onClick={() => onView(card.view)}
            style={{ animationDelay: `${index * 35}ms` }}
          >
            <span className="launcher-icon">
              <card.icon />
            </span>
            <span className="launcher-copy">
              <strong>{t(card.title)}</strong>
              <span>{t(card.hint)}</span>
              <small>
                {card.view === 'activity' && data.items.some((item) => !finished(item)) && (
                  <i className="activity-dot" />
                )}
                {card.badge}
              </small>
            </span>
            <ArrowUpRight className="launcher-arrow" size={18} />
          </button>
        ))}
      </div>
    </div>
  )
}
export function PanelChanges({ changes }: { changes: FileChange[] }) {
  const { t } = useTranslation(),
    [search, setSearch] = useState('')
  return (
    <div className="panel-changes">
      <p className="panel-eyebrow">{t('panelChangesTitle')}</p>
      <h2>{t('changesCount', { count: changes.length })}</h2>
      <p>
        <span className="change-add">+{changes.reduce((n, c) => n + c.added, 0)}</span>{' '}
        <span className="change-del">−{changes.reduce((n, c) => n + c.removed, 0)}</span>
      </p>
      {changes.length > 8 && (
        <input
          aria-label={t('panelSearch')}
          placeholder={t('panelSearch')}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
        />
      )}
      <ChangeList
        changes={changes.filter((c) => c.path.toLowerCase().includes(search.toLowerCase()))}
      />
      {!changes.length && <p className="panel-subtitle">{t('panelEmptyChanges')}</p>}
    </div>
  )
}
const stateKeys = {
  queued: 'panelQueued',
  running: 'panelRunningState',
  waiting: 'panelWaiting',
  paused: 'panelPaused',
  completed: 'panelCompleted',
  failed: 'panelFailed',
  cancelled: 'panelCancelled',
}
function ActivityCard({
  item,
  items,
  now,
  ancestors = [],
}: {
  item: ActivityItem
  items: ActivityItem[]
  now: number
  ancestors?: string[]
}) {
  const { t } = useTranslation()
  const seconds = Math.max(
    0,
    Math.floor(
      ((finished(item) ? Date.parse(item.finished_at ?? item.updated_at) : now) -
        Date.parse(item.started_at)) /
        1000,
    ),
  )
  const Icon = item.kind === 'subagent' ? Bot : item.kind === 'automation' ? Workflow : Activity
  return (
    <li className="activity-node">
      <article className={`activity-card activity-${item.state}`}>
        <div className="activity-card-heading">
          <Icon size={18} />
          <strong>{item.name}</strong>
          {item.profile && <small className="activity-profile">{item.profile}</small>}
          <span className="activity-state">
            {item.state === 'completed' ? (
              <Check size={12} />
            ) : item.state === 'running' ? (
              <i className="activity-dot" />
            ) : null}
            {t(stateKeys[item.state])}
          </span>
        </div>
        {item.goal && <p>{item.goal}</p>}
        {item.step && (
          <p className={finished(item) ? 'activity-step' : 'activity-step live'}>{item.step}</p>
        )}
        {item.progress && (
          <progress
            max={Math.max(1, item.progress.total)}
            value={item.progress.done}
            aria-label={item.name}
          />
        )}
        <time>
          {Math.floor(seconds / 60)}:{String(seconds % 60).padStart(2, '0')}
        </time>
        {item.error && <p role="alert">{item.error}</p>}
      </article>
      {items.some((child) => child.parent_id === item.id && !ancestors.includes(child.id)) && (
        <ul className="activity-children">
          {items
            .filter(
              (child) =>
                child.parent_id === item.id &&
                !ancestors.includes(child.id) &&
                child.id !== item.id,
            )
            .map((child) => (
              <ActivityCard
                key={child.id}
                item={child}
                items={items}
                now={now}
                ancestors={[...ancestors, item.id]}
              />
            ))}
        </ul>
      )}
    </li>
  )
}
export function PanelActivity({ data }: { data: PanelData }) {
  const { t } = useTranslation(),
    [now, setNow] = useState(Date.now)
  useEffect(() => {
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [])
  const items = data.items
    .filter(
      (item) =>
        !finished(item) || now - Date.parse(item.finished_at ?? item.updated_at) < 30 * 60 * 1000,
    )
    .sort(
      (a, b) =>
        Number(finished(a)) - Number(finished(b)) || b.updated_at.localeCompare(a.updated_at),
    )
    .slice(0, 50)
  return (
    <div className="panel-activity">
      <p className="panel-eyebrow">{t('activity')}</p>
      {data.loading && <p role="status">{t('panelLoading')}</p>}
      {data.error && (
        <p role="alert">
          {t('panelActivityError')} <button onClick={data.retry}>{t('panelRetry')}</button>
        </p>
      )}
      {!data.loading && !data.error && !items.length && (
        <div className="activity-empty">
          <Bot size={38} />
          <h2>{t('panelEmptyActivity')}</h2>
          <p>{t('panelEmptyHint')}</p>
        </div>
      )}
      {[false, true].map((recent) => {
        const group = items.filter((item) => finished(item) === recent)
        const roots = group.filter(
          (item) => !group.some((parent) => parent.id === item.parent_id && parent.id !== item.id),
        )
        return group.length ? (
          <section key={String(recent)}>
            <h2>{t(recent ? 'panelRecent' : 'panelCurrent')}</h2>
            <ul>
              {(roots.length ? roots : group.slice(0, 1)).map((item) => (
                <ActivityCard key={item.id} item={item} items={group} now={now} />
              ))}
            </ul>
          </section>
        ) : null
      })}
    </div>
  )
}

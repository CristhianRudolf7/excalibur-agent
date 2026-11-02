import { PendingSpinner } from '@/features/work/WorkSkeleton'
import { useLoadingState } from '@/features/work/loading'
import { WorkSelect } from '@/features/work/WorkSelect'
import { WorkTabs } from '@/features/work/WorkTabs'
import { useWorkEvents } from '@/features/work/events'
import { useCallback, useEffect, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Bell, Plus } from 'lucide-react'
import { Dialog } from '@/components/ui/dialog'
import { rpc } from '@/lib/rpc/client'
import type { Reminder, RpcMethods } from '@/lib/rpc/contract'
import { DataState, VirtualRows } from '@/features/work/shared'
import { formatReminderDate, localInput, reminderGroup, tomorrowAtNine, zonedInput } from './format'
const key = ['work', 'reminders'] as const
type Change =
  | { kind: 'create'; params: RpcMethods['reminders.create']['params'] }
  | { kind: 'update'; params: RpcMethods['reminders.update']['params'] }
  | { kind: 'delete'; params: { id: string } }
function ReminderForm({
  reminder,
  save,
  busy,
}: {
  reminder?: Reminder
  save: (params: RpcMethods['reminders.create']['params']) => void
  busy: boolean
}) {
  const { t } = useTranslation()
  const zone = reminder?.timezone || Intl.DateTimeFormat().resolvedOptions().timeZone
  const [text, setText] = useState(reminder?.text ?? ''),
    [timezone, setTimezone] = useState(zone),
    [date, setDate] = useState(() =>
      localInput(new Date(reminder?.due_at ?? Date.now() + 3600000), zone),
    ),
    [repeat, setRepeat] = useState(reminder?.repeat ?? ''),
    [invalid, setInvalid] = useState(false)
  const [custom, setCustom] = useState(
    Boolean(reminder?.repeat && !['daily', 'weekly'].includes(reminder.repeat)),
  )
  return (
    <form
      className="work-form"
      onSubmit={(e) => {
        e.preventDefault()
        try {
          if (!text.trim() || (custom && repeat.trim().split(/\s+/).length !== 5))
            throw new Error('Invalid input')
          const due_at = zonedInput(date, timezone)
          setInvalid(false)
          save({ text: text.trim(), due_at, timezone, repeat: repeat.trim() })
        } catch {
          setInvalid(true)
        }
      }}
    >
      <label>
        {t('reminderText')}
        <input autoFocus required value={text} onChange={(e) => setText(e.target.value)} />
      </label>
      <label>
        {t('reminderDate')}
        <input
          required
          type="datetime-local"
          value={date}
          onChange={(e) => setDate(e.target.value)}
        />
      </label>
      <label>
        {t('reminderTimezone')}
        <input required value={timezone} onChange={(e) => setTimezone(e.target.value)} />
      </label>
      <div className="work-form-field">
        <span>{t('reminderRepeat')}</span>
        <WorkSelect
          label={t('reminderRepeat')}
          icon="repeat"
          value={custom ? 'custom' : repeat}
          onChange={(value) => {
            setCustom(value === 'custom')
            setRepeat(value === 'custom' ? '' : value)
          }}
          options={[
            { value: '', label: t('reminderNever') },
            { value: 'daily', label: t('reminderDaily') },
            { value: 'weekly', label: t('reminderWeekly') },
            { value: 'custom', label: t('reminderCron') },
          ]}
        />
      </div>
      {custom && (
        <label>
          {t('reminderCron')}
          <input required value={repeat} onChange={(e) => setRepeat(e.target.value)} />
        </label>
      )}
      {invalid && <p role="alert">{t('workInvalid')}</p>}
      <button className="primary-button" aria-busy={busy} disabled={busy} type="submit">
        {busy && <PendingSpinner />} {t('workSave')}
      </button>
    </form>
  )
}
export function Reminders() {
  const { t, i18n } = useTranslation(),
    client = useQueryClient()
  const [tab, setTab] = useState('upcoming'),
    [editing, setEditing] = useState<Reminder | 'new'>(),
    [snoozing, setSnoozing] = useState<Reminder>(),
    [customDate, setCustomDate] = useState(''),
    [dateError, setDateError] = useState(false),
    [fired, setFired] = useState<Reminder>()
  const onFired = useCallback((reminder: Reminder) => setFired(reminder), [])
  useWorkEvents(onFired)
  useEffect(() => {
    if (!fired) return
    const timer = setTimeout(() => setFired(undefined), 8000)
    return () => clearTimeout(timer)
  }, [fired])
  const query = useQuery({
    queryKey: key,
    queryFn: ({ signal }) => rpc.call('reminders.list', { include_done: true }, { signal }),
    retry: false,
  })
  const queryLoading = useLoadingState(query.isPending)
  const mutation = useMutation({
    mutationFn: async (change: Change) => {
      if (change.kind === 'create') return rpc.call('reminders.create', change.params)
      if (change.kind === 'update') return rpc.call('reminders.update', change.params)
      return rpc.call('reminders.delete', change.params)
    },
    onMutate: async (change) => {
      await client.cancelQueries({ queryKey: key })
      const previous = client.getQueryData<{ reminders: Reminder[] }>(key) ?? { reminders: [] }
      client.setQueryData<{ reminders: Reminder[] }>(key, (old) => {
        const reminders = old?.reminders ?? []
        if (change.kind === 'create')
          return {
            reminders: [
              ...reminders,
              {
                ...change.params,
                timezone: change.params.timezone || 'UTC',
                id: 'optimistic',
                status: 'scheduled',
                created_at: new Date().toISOString(),
              },
            ],
          }
        if (change.kind === 'delete')
          return { reminders: reminders.filter((r) => r.id !== change.params.id) }
        return {
          reminders: reminders.map((r) =>
            r.id === change.params.id
              ? {
                  ...r,
                  ...change.params,
                  due_at: change.params.snooze_until ?? change.params.due_at ?? r.due_at,
                }
              : r,
          ),
        }
      })
      return { previous }
    },
    onError: (_error, _change, context) => client.setQueryData(key, context?.previous),
    onSuccess: () => {
      setEditing(undefined)
      setSnoozing(undefined)
    },
    onSettled: () => client.invalidateQueries({ queryKey: key }),
  })
  const reminders = (query.data?.reminders ?? [])
    .filter((r) =>
      tab === 'upcoming' ? r.status === 'scheduled' || r.status === 'snoozed' : r.status === tab,
    )
    .sort((a, b) => Date.parse(a.due_at) - Date.parse(b.due_at))
  const dayGroups = new Map<string, { label: string; rows: Reminder[] }>()
  for (const reminder of reminders) {
    const date = new Date(reminder.due_at)
    const day = date.toLocaleDateString('en-CA')
    const group = reminderGroup(reminder)
    const label =
      tab !== 'upcoming'
        ? t(tab === 'done' ? 'reminderDone' : 'reminderCancelled')
        : group === 'reminderToday' || group === 'reminderTomorrow'
          ? t(group)
          : date.toLocaleDateString(i18n.language, {
              weekday: 'long',
              month: 'short',
              day: 'numeric',
            })
    const id = tab === 'upcoming' ? day : tab
    const existing = dayGroups.get(id)
    if (existing) existing.rows.push(reminder)
    else dayGroups.set(id, { label, rows: [reminder] })
  }
  const snooze = (reminder: Reminder, due: string) =>
    mutation.mutate({
      kind: 'update',
      params: { id: reminder.id, status: 'snoozed', snooze_until: due },
    })
  return (
    <div className="work-page">
      <header className="work-header">
        <Bell />
        <div>
          <span className="work-eyebrow">{t('workEyebrow')}</span>
          <h1>{t('reminders')}</h1>
          <p>{t('reminderUpcoming')}</p>
        </div>
        <button
          className="primary-button"
          onClick={() => {
            mutation.reset()
            setEditing('new')
          }}
        >
          <Plus />
          {t('reminderNew')}
        </button>
      </header>
      <WorkTabs value={tab} label={t('reminders')}>
        {[
          ['upcoming', 'reminderUpcoming'],
          ['done', 'reminderDone'],
          ['cancelled', 'reminderCancelled'],
        ].map(([value, label]) => (
          <button
            key={value}
            role="tab"
            aria-selected={tab === value}
            onClick={() => setTab(value!)}
          >
            {t(label!)}
          </button>
        ))}
      </WorkTabs>
      {fired && (
        <div className="work-toast" role="status">
          <Bell />
          {t('reminderFired', { text: fired.text })}
        </div>
      )}
      {mutation.isError && (
        <p className="work-error" role="alert">
          {t('workMutationError')}
        </p>
      )}
      <DataState
        shape="reminders"
        loading={queryLoading.visible}
        error={query.isError}
        empty={!queryLoading.holding && !query.isError && !reminders.length}
        retry={() => void query.refetch()}
      />
      <div className="work-reminder-groups">
        {(!queryLoading.holding ? [...dayGroups] : []).map(([group, { label, rows }]) => {
          return (
            <section key={group}>
              <h2>
                {label} <span>{rows.length}</span>
              </h2>
              <VirtualRows
                rows={queryLoading.holding ? [] : rows}
                rowKey={(r) => r.id}
                height={145}
                render={(r) => (
                  <article
                    className={`work-reminder ${tab === 'upcoming' && reminders[0]?.id === r.id ? 'work-next-due' : ''} ${fired?.id === r.id ? 'work-highlight' : ''}`}
                  >
                    <strong>{r.text}</strong>
                    <p>
                      <time>{formatReminderDate(r.due_at, i18n.language, r.timezone)}</time> ·{' '}
                      {r.timezone}
                      {r.repeat && <> · {r.repeat}</>}
                    </p>
                    <div className="work-actions">
                      <button
                        disabled={mutation.isPending}
                        onClick={() => {
                          mutation.reset()
                          setEditing(r)
                        }}
                      >
                        {t('workEdit')}
                      </button>
                      {tab === 'upcoming' && (
                        <>
                          <button
                            disabled={mutation.isPending}
                            onClick={() =>
                              mutation.mutate({
                                kind: 'update',
                                params: { id: r.id, status: 'done' },
                              })
                            }
                          >
                            {t('reminderComplete')}
                          </button>
                          <button
                            disabled={mutation.isPending}
                            onClick={() => {
                              setCustomDate('')
                              setDateError(false)
                              setSnoozing(r)
                            }}
                          >
                            {t('reminderSnooze')}
                          </button>
                          <button
                            disabled={mutation.isPending}
                            onClick={() =>
                              mutation.mutate({
                                kind: 'update',
                                params: { id: r.id, status: 'cancelled' },
                              })
                            }
                          >
                            {t('work_cancelled')}
                          </button>
                        </>
                      )}
                      <button
                        disabled={mutation.isPending}
                        onClick={() => mutation.mutate({ kind: 'delete', params: { id: r.id } })}
                      >
                        {t('reminderDelete')}
                      </button>
                    </div>
                  </article>
                )}
              />
            </section>
          )
        })}
      </div>
      {editing && (
        <Dialog
          className="work-dialog"
          title={editing === 'new' ? 'reminderNew' : 'reminderEdit'}
          onClose={() => setEditing(undefined)}
        >
          {mutation.isError && <p role="alert">{t('workMutationError')}</p>}
          <ReminderForm
            reminder={editing === 'new' ? undefined : editing}
            busy={mutation.isPending}
            save={(params) =>
              mutation.mutate(
                editing === 'new'
                  ? { kind: 'create', params }
                  : { kind: 'update', params: { id: editing.id, ...params } },
              )
            }
          />
        </Dialog>
      )}
      {snoozing && (
        <Dialog
          className="work-dialog"
          title="reminderSnooze"
          onClose={() => setSnoozing(undefined)}
        >
          <div className="work-form">
            {mutation.isError && <p role="alert">{t('workMutationError')}</p>}
            <button
              disabled={mutation.isPending}
              onClick={() => snooze(snoozing, new Date(Date.now() + 900000).toISOString())}
            >
              {t('reminder15')}
            </button>
            <button
              disabled={mutation.isPending}
              onClick={() => snooze(snoozing, new Date(Date.now() + 3600000).toISOString())}
            >
              {t('reminderHour')}
            </button>
            <button
              disabled={mutation.isPending}
              onClick={() => snooze(snoozing, tomorrowAtNine(new Date(), snoozing.timezone))}
            >
              {t('reminderTomorrow')}
            </button>
            <label>
              {t('reminderCustom')}
              <input
                type="datetime-local"
                value={customDate}
                onChange={(e) => setCustomDate(e.target.value)}
              />
            </label>
            {dateError && <p role="alert">{t('workInvalid')}</p>}
            <button
              disabled={mutation.isPending || !customDate}
              onClick={() => {
                try {
                  const due = zonedInput(customDate, snoozing.timezone)
                  if (Date.parse(due) <= Date.now()) throw new Error('Past date')
                  snooze(snoozing, due)
                } catch {
                  setDateError(true)
                }
              }}
            >
              {t('workSave')}
            </button>
          </div>
        </Dialog>
      )}
    </div>
  )
}

/** Shared FE1/FE2 API: describeSchedule(schedule, t, locale) and relativeTime(value, locale, now?)
 * return localized text. Query hooks keep the RPC result wrappers ({automation, versions}, {runs}). */
import { useEffect } from 'react'
import { useMutation, useQuery, useQueryClient, type QueryClient } from '@tanstack/react-query'
import type { TFunction } from 'i18next'
import { rpc } from '@/lib/rpc/client'
import { subscribeEvents } from '@/lib/rpc/events'
import type {
  Automation,
  AutomationRun,
  AutomationSchedule,
  AutomationVersion,
  ConversationEvent,
} from '@/lib/rpc/contract'

export function describeSchedule(s: AutomationSchedule, t: TFunction, locale: string) {
  switch (s.kind) {
    case 'daily':
      return t('autoDaily', { time: s.time })
    case 'weekly':
      return t('autoWeekly', {
        time: s.time,
        days: new Intl.ListFormat(locale, { type: 'conjunction' }).format(
          (s.weekdays ?? []).map((day) =>
            new Intl.DateTimeFormat(locale, { weekday: 'long', timeZone: 'UTC' }).format(
              new Date(Date.UTC(2026, 10, 1 + day)),
            ),
          ),
        ),
      })
    case 'monthly':
      return t('autoMonthly', { day: s.day_of_month, time: s.time })
    case 'interval':
      return t((s.every_minutes ?? 0) % 60 === 0 ? 'autoHours' : 'autoMinutes', {
        count: (s.every_minutes ?? 0) % 60 === 0 ? (s.every_minutes ?? 0) / 60 : s.every_minutes,
      })
    case 'once':
      return t('autoOnce', {
        date: new Intl.DateTimeFormat(locale, {
          dateStyle: 'medium',
          timeStyle: 'short',
          timeZone: s.timezone,
        }).format(new Date(s.at!)),
      })
    case 'cron':
      return t('autoCron', { cron: s.cron })
  }
}
export function relativeTime(value: string, locale: string, now: number = Date.now()) {
  const seconds = (Date.parse(value) - now) / 1000
  const [unit, divisor]: [Intl.RelativeTimeFormatUnit, number] =
    Math.abs(seconds) < 3600
      ? ['minute', 60]
      : Math.abs(seconds) < 86400
        ? ['hour', 3600]
        : ['day', 86400]
  return new Intl.RelativeTimeFormat(locale, { style: 'short', numeric: 'always' }).format(
    Math.round(seconds / divisor),
    unit,
  )
}
export const useAutomations = () =>
  useQuery({ queryKey: ['automations'], queryFn: () => rpc.call('automations.list', {}) })
export const useAutomation = (id: string) =>
  useQuery({ queryKey: ['automation', id], queryFn: () => rpc.call('automations.get', { id }) })
export const useAutomationRuns = (id: string) =>
  useQuery({
    queryKey: ['automation-runs', id],
    queryFn: () => rpc.call('automations.runs', { id }),
  })
export function replaceAutomation(client: QueryClient, automation: Automation) {
  client.setQueryData<{ automations: Automation[] }>(['automations'], (old) =>
    old
      ? {
          automations: [...old.automations.filter((a) => a.id !== automation.id), automation].sort(
            (a, b) => a.name.localeCompare(b.name),
          ),
        }
      : undefined,
  )
  client.setQueryData<{ automation: Automation; versions: AutomationVersion[] }>(
    ['automation', automation.id],
    (old) => (old ? { ...old, automation } : undefined),
  )
}
export function applyAutomationEvent(
  client: QueryClient,
  event: Pick<ConversationEvent, 'type' | 'payload'>,
) {
  const payload = event.payload
  if (event.type === 'automation.updated' && 'automation' in payload && payload.automation) {
    replaceAutomation(client, payload.automation)
    void client.invalidateQueries({ queryKey: ['automation', payload.automation.id] })
  }
  if (event.type === 'automation.deleted' && 'id' in payload && typeof payload.id === 'string') {
    const id = payload.id
    client.setQueryData<{ automations: Automation[] }>(['automations'], (old) =>
      old ? { automations: old.automations.filter((a) => a.id !== id) } : undefined,
    )
    client.removeQueries({ queryKey: ['automation', id] })
    client.removeQueries({ queryKey: ['automation-runs', id] })
  }
  if (event.type === 'automation.run' && 'run' in payload && payload.run) {
    const run = payload.run
    client.setQueryData<{ runs: AutomationRun[] }>(
      ['automation-runs', run.automation_id],
      (old) => ({
        runs: [run, ...(old?.runs ?? []).filter((r) => r.id !== run.id)]
          .sort((a, b) => b.started_at.localeCompare(a.started_at))
          .slice(0, 100),
      }),
    )
    const update = (a: Automation) =>
      a.id === run.automation_id && (!a.last_run || a.last_run.started_at <= run.started_at)
        ? { ...a, last_run: run }
        : a
    client.setQueryData<{ automations: Automation[] }>(['automations'], (old) =>
      old ? { automations: old.automations.map(update) } : undefined,
    )
    client.setQueryData<{ automation: Automation; versions: AutomationVersion[] }>(
      ['automation', run.automation_id],
      (old) => (old ? { ...old, automation: update(old.automation) } : undefined),
    )
  }
}
export function useAutomationEvents(onDeleted: (id: string) => void) {
  const client = useQueryClient()
  useEffect(() => {
    let connected = false
    return subscribeEvents({
      types: ['automation.updated', 'automation.deleted', 'automation.run'],
      ready: () => {
        if (connected)
          for (const key of ['automations', 'automation', 'automation-runs'])
            void client.invalidateQueries({ queryKey: [key] })
        connected = true
      },
      offline: () => {},
      event: (event) => {
        applyAutomationEvent(client, event)
        if (
          event.type === 'automation.deleted' &&
          'id' in event.payload &&
          typeof event.payload.id === 'string'
        )
          onDeleted(event.payload.id)
      },
    })
  }, [client, onDeleted])
}
export function useRunAutomation() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => rpc.call('automations.run', { id }),
    onSuccess: ({ run }) =>
      applyAutomationEvent(client, { type: 'automation.run', payload: { run } }),
  })
}
function useStatusMutation(status: 'active' | 'paused') {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (id: string) =>
      rpc.call(status === 'active' ? 'automations.resume' : 'automations.pause', { id }),
    onMutate: async (id) => {
      await client.cancelQueries({ queryKey: ['automations'] })
      await client.cancelQueries({ queryKey: ['automation', id] })
      const previous = client.getQueryData<{ automations: Automation[] }>(['automations'])
      const detail = client.getQueryData<{ automation: Automation; versions: AutomationVersion[] }>(
        ['automation', id],
      )
      const a = detail?.automation ?? previous?.automations.find((a) => a.id === id)
      if (a) replaceAutomation(client, { ...a, status, next_run_at: undefined })
      return { previous, detail, id }
    },
    onError: (_error, _id, context) => {
      if (context?.previous) client.setQueryData(['automations'], context.previous)
      if (context?.detail) client.setQueryData(['automation', context.id], context.detail)
    },
    onSuccess: (a) => replaceAutomation(client, a),
    onSettled: () => client.invalidateQueries({ queryKey: ['automations'] }),
  })
}
export const usePauseAutomation = () => useStatusMutation('paused')
export const useResumeAutomation = () => useStatusMutation('active')
export function useDeleteAutomation() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: (id: string) => rpc.call('automations.delete', { id }),
    onSuccess: ({ id }) =>
      applyAutomationEvent(client, { type: 'automation.deleted', payload: { id } }),
  })
}

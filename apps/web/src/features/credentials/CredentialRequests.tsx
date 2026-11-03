import { useEffect, useLayoutEffect, useRef } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { rpc } from '@/lib/rpc/client'
import { subscribeEvents } from '@/lib/rpc/events'
import type {
  CredentialRequest,
  CredentialRequestedEvent,
  CredentialStatus,
} from '@/lib/rpc/credentials'
import { Button } from '@/components/ui/button'
import { CredentialCard } from './CredentialCard'
import { credentialMetadata } from './metadata'

export function CredentialRequests({
  conversationId,
  focusedId,
  onChange,
}: {
  conversationId: string
  focusedId?: string
  onChange?: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const statuses = useRef(new Map<string, Exclude<CredentialStatus, 'pending'>>())
  const revision = useRef(0)
  const received = useRef(new Map<string, number>())
  const key = ['credentials', conversationId] as const
  const requests = useQuery({
    queryKey: key,
    queryFn: async ({ signal }) => {
      const started = revision.current
      const result = await rpc.call(
        'credentials.pending',
        { conversation_id: conversationId },
        { signal },
      )
      const previous = client.getQueryData<CredentialRequest[]>(key) ?? []
      const rows = new Map(
        previous
          .filter(
            (request) =>
              request.status !== 'pending' || (received.current.get(request.id) ?? 0) > started,
          )
          .map((request) => [request.id, request]),
      )
      const cached = new Map(previous.map((request) => [request.id, request]))
      for (const request of result.requests ?? [])
        rows.set(request.id, {
          ...credentialMetadata(request),
          status:
            statuses.current.get(request.id) ??
            (cached.get(request.id)?.status !== 'pending'
              ? cached.get(request.id)?.status
              : undefined) ??
            request.status,
        })
      return [...rows.values()]
    },
    retry: false,
  })
  useLayoutEffect(() => {
    onChange?.()
  }, [requests.data, onChange])
  const resolve = (id: string, status: Exclude<CredentialStatus, 'pending'>) => {
    statuses.current.set(id, status)
    client.setQueryData<CredentialRequest[]>(key, (rows = []) =>
      rows.map((request) => (request.id === id ? { ...request, status } : request)),
    )
  }
  useEffect(
    () =>
      subscribeEvents({
        types: ['credentials.requested', 'credentials.resolved'],
        ready: () => {
          void client.invalidateQueries({ queryKey: ['credentials', conversationId] })
        },
        offline: () => {},
        event: (event) => {
          if (event.type === 'credentials.requested') {
            const payload = event.payload as unknown as CredentialRequestedEvent
            if (payload.conversation_id !== conversationId) return
            received.current.set(payload.request_id, ++revision.current)
            const cached = client
              .getQueryData<CredentialRequest[]>(['credentials', conversationId])
              ?.find((row) => row.id === payload.request_id)
            const request: CredentialRequest = {
              id: payload.request_id,
              conversation_id: payload.conversation_id,
              site: payload.site,
              purpose: payload.purpose,
              fields: payload.fields,
              remember_default: payload.remember_default,
              created_at: payload.created_at,
              status: statuses.current.get(payload.request_id) ?? cached?.status ?? 'pending',
            }
            client.setQueryData<CredentialRequest[]>(
              ['credentials', conversationId],
              (rows = []) => [...rows.filter((row) => row.id !== request.id), request],
            )
          } else if (event.type === 'credentials.resolved') {
            const payload = event.payload as unknown as {
              request_id: string
              status: Exclude<CredentialStatus, 'pending'>
            }
            statuses.current.set(payload.request_id, payload.status)
            client.setQueryData<CredentialRequest[]>(['credentials', conversationId], (rows = []) =>
              rows.map((row) =>
                row.id === payload.request_id ? { ...row, status: payload.status } : row,
              ),
            )
          }
        },
      }),
    [client, conversationId],
  )
  return (
    <>
      {requests.isError && (
        <div role="alert">
          {t('credentialLoadError')}{' '}
          <Button onClick={() => void requests.refetch()}>{t('credentialRetry')}</Button>
        </div>
      )}
      {focusedId &&
        requests.isSuccess &&
        !requests.data.some((request) => request.id === focusedId) && (
          <p role="status">{t('credentialMissing')}</p>
        )}
      {requests.data?.map((request) => (
        <CredentialCard
          key={request.id}
          request={request}
          focused={request.id === focusedId}
          onResolved={(status) => resolve(request.id, status)}
        />
      ))}
    </>
  )
}

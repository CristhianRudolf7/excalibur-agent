import { useEffect } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { rpc } from '@/lib/rpc/client'
import { Chat } from '@/features/chat/Chat'
import { Button } from '@/components/ui/button'
import { credentialMetadata } from './metadata'

/** Empty conversation filter discovers a request arriving from another channel. */
export function CredentialConversation({
  agentId,
  credentialId,
  conversation,
  onFile,
}: {
  agentId: string
  credentialId: string
  conversation?: string
  onFile: () => void
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const pending = useQuery({
    queryKey: ['credential-link', credentialId],
    queryFn: async ({ signal }) => {
      const { requests } = await rpc.call(
        'credentials.pending',
        { conversation_id: '' },
        { signal },
      )
      const request = requests?.find((request) => request.id === credentialId)
      return request ? credentialMetadata(request) : null
    },
    enabled: !conversation,
    staleTime: Infinity,
    retry: false,
  })
  const target = conversation ?? pending.data?.conversation_id
  useEffect(() => {
    if (conversation || !target) return
    void navigate({
      to: '/agents/$agentId',
      params: { agentId },
      replace: true,
      search: (previous) => ({
        ...previous,
        panel: previous.panel ?? 'home',
        view: 'chat',
        credential: credentialId,
        conversation: target,
      }),
    })
  }, [navigate, agentId, target, conversation, credentialId])
  if (target)
    return (
      <Chat
        key={target}
        agentId={agentId}
        conversation={target}
        credentialId={credentialId}
        onFile={onFile}
      />
    )
  return (
    <div className="grid gap-3 p-6" role={pending.isError ? 'alert' : 'status'}>
      {t(
        pending.isError
          ? 'credentialLoadError'
          : pending.isPending
            ? 'settingsLoading'
            : 'credentialMissing',
      )}
      {pending.isError && (
        <Button onClick={() => void pending.refetch()}>{t('credentialRetry')}</Button>
      )}
    </div>
  )
}

import { useId, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Star } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { AgentAvatar } from '@/components/AgentAvatar'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { rpc } from '@/lib/rpc/client'
import { agentsKey, applyAgentList, useAgentsQuery } from './api'
import { orderAgents, useAgents } from './store'
import { useOpenAgentChat } from './navigation'
import type { Agent, AgentFields } from './types'

type Action =
  | { kind: 'archive'; id: string }
  | { kind: 'restore'; id: string }
  | { kind: 'favorite'; id: string }
  | { kind: 'update'; id: string; fields: AgentFields }
function useAgentAction() {
  const client = useQueryClient()
  return useMutation({
    mutationFn: async (action: Action) => {
      // Apply only confirmed server results; a failed operation keeps its form open.
      if (action.kind === 'archive' || action.kind === 'favorite') {
        const result =
          action.kind === 'archive'
            ? await rpc.call('agents.archive', { id: action.id })
            : await rpc.call('agents.set_favorite', { id: action.id })
        const state = useAgents.getState()
        applyAgentList(client, {
          favorite_id: result.favorite_id,
          agents: state.agents.map((agent) =>
            action.kind === 'archive' && agent.id === action.id
              ? { ...agent, archived: true }
              : agent,
          ),
        })
      } else {
        const result =
          action.kind === 'restore'
            ? await rpc.call('agents.restore', { id: action.id })
            : await rpc.call('agents.update', { id: action.id, ...action.fields })
        const state = useAgents.getState()
        applyAgentList(client, {
          favorite_id: state.favorite_id,
          agents: state.agents.map((agent) => (agent.id === result.id ? result : agent)),
        })
      }
      void client.invalidateQueries({ queryKey: agentsKey })
    },
  })
}
function AgentEditor({
  agent,
  busy,
  onSave,
}: {
  agent: Agent
  busy: boolean
  onSave: (fields: AgentFields) => void
}) {
  const { t } = useTranslation()
  const prefix = useId()
  const [fields, setFields] = useState<AgentFields>({
    name: agent.name,
    title: agent.title,
    description: agent.description,
    personality: agent.personality ?? '',
  })
  return (
    <form
      className="agent-edit-form"
      onSubmit={(event) => {
        event.preventDefault()
        if (fields.name.trim()) onSave({ ...fields, name: fields.name.trim() })
      }}
    >
      {(['name', 'title', 'description', 'personality'] as const).map((field) => {
        const labels = {
          name: 'agentName',
          title: 'agentTitle',
          description: 'agentDescription',
          personality: 'agentPersonality',
        }
        const id = `${prefix}-${field}`
        return (
          <label htmlFor={id} key={field}>
            {t(labels[field])}
            {field === 'name' || field === 'title' ? (
              <input
                id={id}
                value={fields[field] ?? ''}
                required={field === 'name'}
                disabled={busy}
                onChange={(event) => setFields({ ...fields, [field]: event.target.value })}
              />
            ) : (
              <textarea
                id={id}
                value={fields[field] ?? ''}
                rows={3}
                disabled={busy}
                onChange={(event) => setFields({ ...fields, [field]: event.target.value })}
              />
            )}
          </label>
        )
      })}
      <Button type="submit" variant="primary" disabled={busy || !fields.name.trim()}>
        {t('agentSave')}
      </Button>
    </form>
  )
}
function AvatarRequest({ agent, onRequest }: { agent: Agent; onRequest: (text: string) => void }) {
  const { t } = useTranslation()
  const id = useId()
  const [description, setDescription] = useState('')
  return (
    <form
      className="agent-edit-form"
      onSubmit={(event) => {
        event.preventDefault()
        if (description.trim())
          onRequest(t('agentAvatarRequest', { name: agent.name, description: description.trim() }))
      }}
    >
      <AgentAvatar spec={agent.avatar} size={64} />
      <label htmlFor={id}>
        {t('agentAvatarPrompt')}
        <textarea
          id={id}
          value={description}
          onChange={(event) => setDescription(event.target.value)}
          required
          rows={3}
        />
      </label>
      <Button type="submit" disabled={!description.trim()}>
        {t('agentAvatarOpen')}
      </Button>
    </form>
  )
}
export function AgentSettings() {
  const { t } = useTranslation()
  const query = useAgentsQuery()
  const agents = useAgents((state) => state.agents)
  const action = useAgentAction()
  const openChat = useOpenAgentChat()
  const [dialog, setDialog] = useState<{ kind: 'edit' | 'archive' | 'avatar'; id: string }>()
  const active = orderAgents(agents)
  const archived = agents.filter((agent) => agent.archived)
  const target = agents.find((agent) => agent.id === dialog?.id)
  const open = (kind: NonNullable<typeof dialog>['kind'], id: string) => {
    action.reset()
    setDialog({ kind, id })
  }
  const perform = (command: Action) => {
    void action
      .mutateAsync(command)
      .then(() => setDialog(undefined))
      .catch(() => {})
  }
  const failure = <p role="alert">{t('agentActionError')}</p>
  return (
    <div className="agent-settings">
      <p className="settings-description">{t('agentSettingsNote')}</p>
      {query.isPending && <p role="status">{t('settingsLoading')}</p>}
      {query.isError && (
        <p role="alert">
          {t('agentLoadError')}{' '}
          <Button onClick={() => void query.refetch()}>{t('agentRetry')}</Button>
        </p>
      )}
      {action.isError && !dialog && failure}
      {!query.isPending &&
        active.map((agent) => (
          <article
            className="settings-card agent-settings-card"
            key={agent.id}
            aria-label={agent.name}
          >
            <div className="agent-settings-heading">
              <AgentAvatar spec={agent.avatar} size={46} />
              <div>
                <h3>{agent.name}</h3>
                {agent.title && <p>{agent.title}</p>}
              </div>
              {agent.favorite && (
                <span className="agent-favorite-label">
                  <Star size={14} />
                  {t('agentFavorite')}
                </span>
              )}
            </div>
            {agent.description && <p>{agent.description}</p>}
            <div className="agent-settings-actions">
              {!agent.favorite && (
                <Button
                  disabled={action.isPending}
                  onClick={() => perform({ kind: 'favorite', id: agent.id })}
                >
                  {t('agentSetFavorite')}
                </Button>
              )}
              <Button disabled={action.isPending} onClick={() => open('edit', agent.id)}>
                {t('agentEdit')}
              </Button>
              <Button disabled={action.isPending} onClick={() => open('avatar', agent.id)}>
                {t('agentRegenerate')}
              </Button>
              <Button
                disabled={action.isPending || active.length <= 1}
                title={active.length <= 1 ? t('agentLastActive') : undefined}
                onClick={() => open('archive', agent.id)}
              >
                {t('agentArchive')}
              </Button>
            </div>
            {active.length <= 1 && <small>{t('agentLastActive')}</small>}
          </article>
        ))}
      <Button onClick={() => openChat(t('agentCreateRequest'))}>{t('agentCreate')}</Button>
      <h3 className="agent-archived-heading">{t('agentArchived')}</h3>
      {archived.length === 0 && <p>{t('agentNoArchived')}</p>}
      {archived.map((agent) => (
        <article
          className="settings-card agent-settings-heading"
          key={agent.id}
          aria-label={agent.name}
        >
          <AgentAvatar spec={agent.avatar} size={38} />
          <h3>{agent.name}</h3>
          <Button
            disabled={action.isPending}
            onClick={() => perform({ kind: 'restore', id: agent.id })}
          >
            {t('agentRestore')}
          </Button>
        </article>
      ))}
      {dialog && target && (
        <Dialog
          title={
            dialog.kind === 'archive'
              ? t('agentArchiveTitle', { name: target.name })
              : dialog.kind === 'avatar'
                ? t('agentRegenerate')
                : t('agentEdit')
          }
          onClose={() => {
            if (!action.isPending) setDialog(undefined)
          }}
          footer={false}
        >
          {dialog.kind === 'edit' ? (
            <AgentEditor
              agent={target}
              busy={action.isPending}
              onSave={(fields) => perform({ kind: 'update', id: target.id, fields })}
            />
          ) : dialog.kind === 'avatar' ? (
            <AvatarRequest
              agent={target}
              onRequest={(text) => {
                setDialog(undefined)
                openChat(text)
              }}
            />
          ) : (
            <>
              <p>{t('agentArchiveNote')}</p>
              {target.favorite && <p>{t('agentArchiveFavorite')}</p>}
              {active.length <= 1 && <p>{t('agentLastActive')}</p>}
              <Button
                variant="primary"
                disabled={action.isPending || active.length <= 1}
                onClick={() => perform({ kind: 'archive', id: target.id })}
              >
                {t('agentArchive')}
              </Button>
            </>
          )}
          {action.isError && failure}
          <Button disabled={action.isPending} onClick={() => setDialog(undefined)}>
            {t('agentCancel')}
          </Button>
        </Dialog>
      )}
    </div>
  )
}

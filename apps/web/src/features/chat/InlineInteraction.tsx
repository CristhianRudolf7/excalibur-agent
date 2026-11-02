import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { rpc } from '@/lib/rpc/client'
import { useChat } from './store'
import type { Message } from './types'

export function InlineInteraction({ message, agentId }: { message: Message; agentId: string }) {
  const { t } = useTranslation()
  const [answer, setAnswer] = useState('')
  const [busy, setBusy] = useState(false)
  const [error, setError] = useState(false)
  const interaction = message.interaction!
  const resolve = async (approve = true) => {
    setBusy(true)
    setError(false)
    try {
      if (interaction.kind === 'question')
        await rpc.call('question.answer', { question_id: interaction.id, answer })
      else await rpc.call(approve ? 'plan.approve' : 'plan.deny', { plan_id: interaction.id })
      useChat
        .getState()
        .patch(agentId, message.id, { interaction: { ...interaction, resolved: true } })
    } catch {
      setError(true)
    } finally {
      setBusy(false)
    }
  }
  if (interaction.resolved) return <small role="status">{t('chatAnswered')}</small>
  return (
    <form
      className="chat-interaction"
      onSubmit={(event) => {
        event.preventDefault()
        void resolve()
      }}
    >
      {interaction.kind === 'question' && (
        <input
          aria-label={t('chatAnswer')}
          value={answer}
          onChange={(event) => setAnswer(event.target.value)}
        />
      )}
      <Button type="submit" disabled={busy || (interaction.kind === 'question' && !answer.trim())}>
        {t(interaction.kind === 'question' ? 'chatAnswer' : 'approve')}
      </Button>
      {interaction.kind === 'plan' && (
        <Button type="button" disabled={busy} onClick={() => void resolve(false)}>
          {t('chatDeny')}
        </Button>
      )}
      {error && <small role="alert">{t('chatActionError')}</small>}
    </form>
  )
}

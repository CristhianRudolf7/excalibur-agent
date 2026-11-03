import { useAgents } from '@/features/agents/store'
import { useState } from 'react'
import { useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { Play, Pause, Trash2, Pencil } from 'lucide-react'
import type { Automation } from '@/lib/rpc/contract'
import { Dialog } from '@/components/ui/dialog'
import {
  useRunAutomation,
  usePauseAutomation,
  useResumeAutomation,
  useDeleteAutomation,
} from './api'
import { useAutomationContext } from './context'
export function AutomationActions({
  automation: a,
  compact = false,
}: {
  automation: Automation
  compact?: boolean
}) {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const run = useRunAutomation(),
    pause = usePauseAutomation(),
    resume = useResumeAutomation(),
    remove = useDeleteAutomation()
  const [confirm, setConfirm] = useState(false)
  const busy = run.isPending || pause.isPending || resume.isPending || remove.isPending
  const error = run.error || pause.error || resume.error || remove.error
  return (
    <div className="auto-actions" onClick={(e) => e.stopPropagation()}>
      {[
        {
          label: 'autoRun',
          Icon: Play,
          disabled: busy || a.last_run?.status === 'running',
          click: () => run.mutate(a.id),
        },
        {
          label: a.status === 'active' ? 'autoPause' : 'autoResume',
          Icon: a.status === 'active' ? Pause : Play,
          disabled: busy,
          click: () => (a.status === 'active' ? pause.mutate(a.id) : resume.mutate(a.id)),
        },
        {
          label: 'autoEdit',
          Icon: Pencil,
          disabled: busy,
          click: () => {
            useAutomationContext.getState().attach(a)
            void navigate({
              to: '/agents/$agentId',
              params: { agentId: useAgents.getState().favorite_id },
              search: { view: 'chat', panel: 'files' },
            })
          },
        },
        { label: 'autoDelete', Icon: Trash2, disabled: busy, click: () => setConfirm(true) },
      ].map(({ label, Icon, disabled, click }) => (
        <button
          type="button"
          key={label}
          className="auto-button"
          title={t(label)}
          aria-label={t(label)}
          disabled={disabled}
          onClick={click}
        >
          <Icon size={16} />
          {!compact && t(label)}
        </button>
      ))}
      {error && <p role="alert">{error.message}</p>}
      {confirm && (
        <Dialog title="autoConfirm" onClose={() => setConfirm(false)}>
          <p>{a.name}</p>
          <p>{t('autoDeleteHint')}</p>
          {remove.error && <p role="alert">{remove.error.message}</p>}
          <button
            className="auto-button"
            disabled={busy}
            onClick={() =>
              remove.mutate(a.id, {
                onSuccess: () => {
                  setConfirm(false)
                  if (window.location.pathname !== '/automations')
                    void navigate({ to: '/automations' })
                },
              })
            }
          >
            {t('autoDelete')}
          </button>
        </Dialog>
      )}
    </div>
  )
}

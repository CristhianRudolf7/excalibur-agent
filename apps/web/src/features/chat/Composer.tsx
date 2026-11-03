import { useAgents } from '@/features/agents/store'
import { automationPrefix, useAutomationContext } from '@/features/automations/context'
import { Workflow, X } from 'lucide-react'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { libraryEnabled } from '@/features/library/api'
import { AttachmentChips } from '@/features/library/Attachments'
import { attachmentText } from '@/features/library/attachment-text'
import { useAttachments } from '@/features/library/use-attachments'
import { composerHeight, shouldSend } from './composer-logic'
import { useTranslation } from 'react-i18next'
import { Plus, Mic, ArrowUp, Square } from 'lucide-react'
import { IconButton } from '@/components/shared'
import { CallControls, useCallElapsed } from './voice/CallOverlay'
import { callActive, useCall } from './voice/callStore'
export function Composer({
  agent,
  agentId,
  typing,
  onSend,
  onStop,
  onSendDocuments,
}: {
  agent: string
  agentId?: string
  typing: boolean
  onSend: (text: string) => void
  onStop: () => void
  onSendDocuments?: (text: string, documentIds: string[]) => void
}) {
  const { t, i18n } = useTranslation()
  const onCall = useCall((state) => callActive(state.snapshot) || state.connecting)
  const showCall = useCall((state) => state.open)
  const elapsed = useCallElapsed()
  const context = useAutomationContext()
  const area = useRef<HTMLDivElement>(null)
  // The composer floats over the conversation: reserve exactly its height below
  // the messages and keep the newest message visible while it grows.
  useEffect(() => {
    const node = area.current
    const body = node?.closest<HTMLElement>('.chat-body')
    const messages = body?.querySelector<HTMLElement>('.messages')
    if (!node || !body || typeof ResizeObserver === 'undefined') return
    const update = () => {
      const atBottom = messages
        ? messages.scrollHeight - messages.scrollTop - messages.clientHeight < 40
        : false
      body.style.setProperty('--composer-h', `${Math.ceil(node.offsetHeight)}px`)
      if (atBottom && messages) messages.scrollTop = messages.scrollHeight
    }
    update()
    const observer = new ResizeObserver(update)
    observer.observe(node)
    return () => observer.disconnect()
  }, [])
  const attachments = useAttachments()
  const ready = attachments.items.flatMap((item) =>
    item.document && ['ready', 'partial'].includes(item.document.status) ? [item.document] : [],
  )
  const waiting = attachments.items.some(
    (item) => !item.document || !['ready', 'partial'].includes(item.document.status),
  )
  const [text, setText] = useState(() => useAutomationContext.getState().draft)
  const textarea = useRef<HTMLTextAreaElement>(null)
  const prefill = useAgents((state) => (agentId ? state.drafts[agentId] : undefined))
  useLayoutEffect(() => {
    if (!prefill || !agentId) return
    // oxlint-disable-next-line react/set-state-in-effect -- Apply an external navigation request to the mounted composer before focusing it.
    setText(prefill.text)
    useAutomationContext.getState().clear()
    textarea.current?.focus()
    useAgents.getState().clearPrefill(agentId, prefill.request)
  }, [prefill, agentId])
  useLayoutEffect(() => {
    if (context.automation || context.draft) textarea.current?.focus()
  }, [context.automation, context.draft])
  const file = useRef<HTMLInputElement>(null)
  const [notice, setNotice] = useState('')
  useLayoutEffect(() => {
    const node = textarea.current
    if (node) {
      node.style.height = '24px'
      node.style.height = `${composerHeight(node.scrollHeight, window.innerHeight)}px`
    }
  }, [text])
  const send = () => {
    if ((text.trim() || ready.length) && !typing && !waiting) {
      if (onSendDocuments)
        onSendDocuments(
          automationPrefix(
            attachmentText(text, ready, t('libraryAttached')),
            context.automation,
            t,
            i18n.language,
          ),
          ready.map((document) => document.id),
        )
      else
        onSend(
          automationPrefix(
            attachmentText(text, ready, t('libraryAttached')),
            context.automation,
            t,
            i18n.language,
          ),
        )
      context.clear()
      attachments.clear()
      setText('')
      textarea.current?.focus()
    }
  }
  return (
    <div className="composer-area" ref={area}>
      {notice && (
        <div className="composer-notice" role="status">
          {notice}
        </div>
      )}
      {context.automation && (
        <div className="auto-context-chip">
          <Workflow size={16} />
          {t('autoContext', { name: context.automation.name, version: context.automation.version })}
          <button aria-label={t('autoRemove')} onClick={context.clear}>
            <X size={14} />
          </button>
        </div>
      )}
      <AttachmentChips items={attachments.items} onRemove={attachments.remove} />
      <div className="composer">
        <textarea
          ref={textarea}
          value={text}
          rows={1}
          aria-label={t('writeTo', { agent })}
          placeholder={context.automation ? t('autoChangeHint') : t('writeTo', { agent })}
          onChange={(e) => setText(e.target.value)}
          onKeyDown={(event) => {
            if (shouldSend(event)) {
              event.preventDefault()
              send()
            }
          }}
        />
        <div className="composer-toolbar">
          <IconButton
            label="attach"
            disabled={!libraryEnabled}
            onClick={() => file.current?.click()}
          >
            <Plus />
          </IconButton>
          <input
            hidden
            type="file"
            multiple
            ref={file}
            onChange={(e) => {
              if (e.target.files) attachments.add(e.target.files)
              e.target.value = ''
            }}
          />
          <span className="toolbar-spacer" />
          {onCall ? (
            <>
              <button
                type="button"
                className="composer-call-live"
                aria-label={t('callShow')}
                title={t('callShow')}
                onClick={showCall}
              >
                <span className="composer-call-dot" aria-hidden="true" />
                {t('callLive')} · {elapsed}
              </button>
              <CallControls compact />
            </>
          ) : (
            <>
              <IconButton label="microphone" onClick={() => setNotice(t('unavailable'))}>
                <Mic />
              </IconButton>
              <IconButton
                label={typing ? 'stop' : 'send'}
                className="send-button"
                disabled={!typing && (waiting || (!text.trim() && !ready.length))}
                onClick={typing ? onStop : send}
              >
                {typing ? <Square size={15} fill="currentColor" /> : <ArrowUp />}
              </IconButton>
            </>
          )}
        </div>
      </div>
    </div>
  )
}

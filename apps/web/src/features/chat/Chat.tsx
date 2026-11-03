import type { ClassroomContext } from '@/lib/rpc/classroom'
import { ChangesCard } from './ChangesCard'
import { visibleAutomationMessage } from '@/features/automations/context'
import { InlineInteraction } from './InlineInteraction'
import './chat.css'
import { useEffect, useLayoutEffect, useRef, useState } from 'react'
import { useVirtualizer } from '@tanstack/react-virtual'
import { useTranslation } from 'react-i18next'
import { ChatHistorySkeleton } from './Skeletons'
import { useLoading } from '@/components/shared'
import { PixelAvatar } from '@/components/shared'
import { Button } from '@/components/ui/button'
import { getAgent } from '@/features/shell/mock-data'
import { useChat } from './store'
import { Markdown } from '@/components/markdown/Markdown'
import { Composer } from './Composer'
import { MessageCopy } from './MessageCopy'
import { ArrowUpRight } from 'lucide-react'
import { connectConversation, loadHistory, sendMessage, liveChatEnabled } from './live'
export function Chat({
  agentId,
  onFile,
  context,
  showWelcome = true,
}: {
  agentId: string
  onFile: () => void
  context?: ClassroomContext
  showWelcome?: boolean
}) {
  const { t } = useTranslation()
  const agent = getAgent(agentId)
  const initialHistory = useChat((s) => s.live[agentId])
  const initialError = useChat((s) => s.historyError[agentId])
  const loading = useLoading(liveChatEnabled && !initialHistory && !initialError)
  const [paging, setPaging] = useState(false)
  const pageLoading = useLoading(paging)
  const activity = useChat((s) => s.activity[agentId])
  const online = useChat((s) => s.online[agentId] ?? false)
  const live = useChat((s) => s.live[agentId])
  const older = useChat((s) => s.older[agentId])
  const historyError = useChat((s) => s.historyError[agentId])
  const typing = useChat((s) => s.typing[agentId] ?? false)
  const setTyping = useChat((s) => s.setTyping)
  const parent = useRef<HTMLDivElement>(null)
  const atBottom = useRef(true)
  const conversation = live ?? []
  useEffect(() => connectConversation(agentId), [agentId])
  // oxlint-disable-next-line react/incompatible-library -- Virtualizer measurements intentionally remain outside React Compiler memoization.
  const virtual = useVirtualizer({
    count: conversation.length,
    getItemKey: (index) => conversation[index]!.id,
    getScrollElement: () => parent.current,
    estimateSize: (index) => (conversation[index]?.interaction ? 180 : 100),
    overscan: 4,
  })
  const displayedMessages = conversation
  const previousLength = useRef(0)
  useLayoutEffect(() => {
    if (
      atBottom.current &&
      previousLength.current > 0 &&
      displayedMessages.length > previousLength.current
    )
      virtual.scrollToIndex(displayedMessages.length - 1, { align: 'end' })
    previousLength.current = displayedMessages.length
  }, [displayedMessages, virtual])
  const send = (text: string) => {
    atBottom.current = true
    void sendMessage(agentId, text, [], undefined, undefined, context)
  }
  const stop = () => {
    void sendMessage(agentId, '/stop', [], undefined, undefined, context)
    setTyping(agentId, false)
  }
  return (
    <div className="chat-body">
      {!online && !loading.busy && (
        <div className="chat-connection" role="status">
          <span className="chat-connection-dot" aria-hidden="true" />
          {t('chatReconnecting')}
        </div>
      )}
      {older && (
        <Button
          aria-busy={pageLoading.busy}
          disabled={pageLoading.busy}
          onClick={async () => {
            atBottom.current = false
            setPaging(true)
            try {
              await loadHistory(agentId, older)
            } finally {
              setPaging(false)
            }
          }}
        >
          {t('chatOlder')}
        </Button>
      )}
      {historyError && (
        <div className="chat-connection" role="alert">
          {t('chatHistoryError')}{' '}
          <Button onClick={() => void loadHistory(agentId, older)}>{t('chatRetry')}</Button>
        </div>
      )}
      <div
        ref={parent}
        className="messages"
        onScroll={() => {
          const node = parent.current
          if (node) atBottom.current = node.scrollHeight - node.scrollTop - node.clientHeight < 100
        }}
      >
        {loading.visible && <ChatHistorySkeleton />}
        {pageLoading.visible && <ChatHistorySkeleton compact />}
        {showWelcome && online && live && conversation.length === 0 && !typing && (
          <div className="chat-empty">
            <span className="chat-empty-icon" aria-hidden="true">
              <PixelAvatar id={agent.id} size={56} />
            </span>
            <span className="chat-eyebrow">{t('chatWelcomeLabel')}</span>
            <h2>{t('chatWelcome')}</h2>
            <p>{t('chatWelcomeHint')}</p>
            <div className="chat-suggestions">
              {['chatSuggestPlan', 'chatSuggestCode', 'chatSuggestResearch'].map((key) => (
                <button key={key} type="button" onClick={() => send(t(key))}>
                  {t(key)}
                  <ArrowUpRight size={15} aria-hidden="true" />
                </button>
              ))}
            </div>
          </div>
        )}
        {!loading.busy && conversation.length > 0 && (
          <div className="date-separator">
            <span />
            {t('today')}
            <span />
          </div>
        )}
        <div
          className="message-history"
          style={{ height: virtual.getTotalSize(), position: 'relative' }}
        >
          {!loading.busy &&
            virtual.getVirtualItems().map((item) => {
              const message = conversation[item.index]!
              return (
                <article
                  key={message.id}
                  data-index={item.index}
                  ref={virtual.measureElement}
                  className={`message message-${message.sender}`}
                  style={{
                    position: 'absolute',
                    top: 0,
                    left: 0,
                    width: '100%',
                    transform: `translateY(${item.start}px)`,
                  }}
                >
                  {message.sender === 'agent' && <PixelAvatar id={agent.id} size={32} />}
                  <div className="message-content">
                    {message.sender === 'agent' && (
                      <div className="message-author">
                        <strong>{t(agent.name, { defaultValue: agent.name })}</strong>
                        <time>{message.time}</time>
                      </div>
                    )}
                    {message.sender === 'agent' ? (
                      <Markdown sources={message.sources}>
                        {message.literal ? message.text : t(message.text)}
                      </Markdown>
                    ) : (
                      <p>
                        {visibleAutomationMessage(
                          message.literal ? message.text : t(message.text),
                          t,
                        )}
                      </p>
                    )}
                    {message.sender === 'agent' && !!message.changes?.length && (
                      <ChangesCard changes={message.changes} onFile={onFile} />
                    )}
                    {message.sender === 'agent' && (
                      <MessageCopy text={message.literal ? message.text : t(message.text)} />
                    )}
                    {message.sender === 'user' && <time>{message.time}</time>}
                    {message.delivery === 'sending' && (
                      <small role="status">{t('chatSending')}</small>
                    )}
                    {message.delivery === 'failed' && (
                      <div role="alert">
                        {message.error}
                        <Button
                          onClick={() =>
                            void sendMessage(agentId, message.text, message.attachments, message)
                          }
                        >
                          {t('chatRetry')}
                        </Button>
                      </div>
                    )}
                    {message.interaction && (
                      <InlineInteraction message={message} agentId={agentId} />
                    )}
                  </div>
                </article>
              )
            })}
        </div>
      </div>
      {typing && (
        <div className="chat-activity" role="status">
          <span className="thinking-dots" aria-hidden="true">
            <i />
            <i />
            <i />
          </span>
          {t(activity ?? 'chatThinking')}
        </div>
      )}
      <Composer
        key={agentId}
        agent={agent.name}
        typing={typing}
        onSend={send}
        onSendDocuments={(text, ids) => {
          atBottom.current = true
          void sendMessage(agentId, text, ids, undefined, undefined, context)
        }}
        onStop={stop}
      />
    </div>
  )
}

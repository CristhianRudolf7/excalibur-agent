import { AgentSwitcher } from '@/features/agents/AgentSwitcher'
import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Bot, MessageCircle, Network, Plug, Unplug, X } from 'lucide-react'
import { rpc } from '@/lib/rpc/client'
import { CameraCard } from './CameraCard'
import { IconButton } from '@/components/shared'
import { ResizeHandle } from '@/components/resize/ResizeHandle'
import { Chat } from '@/features/chat/Chat'
import { splitWidths, splitRatioFromPanel } from '@/features/shell/split-layout'
import { PanelToggle } from '@/components/ui/panel-toggle'
import { useShell } from '@/features/shell/store'
import { useRobot } from './api'
import { connectionStatus } from './types'
import '@/styles/redesign/robot.css'

const hostKey = 'excalibur-robot-host-v1'
function rememberedHost() {
  try {
    return localStorage.getItem(hostKey) ?? ''
  } catch {
    return ''
  }
}

export function Robot() {
  const shell = useShell()
  const [chatOpen, setChatOpen] = useState(false)
  const split = useRef<HTMLDivElement>(null)
  const chatPanel = useRef<HTMLElement>(null)
  const [width, setWidth] = useState(0)
  const dimensions = splitWidths(width, shell.ratio)
  const overlay = dimensions.overlay
  useEffect(() => {
    const node = split.current
    if (!node) return
    const observer = new ResizeObserver(([entry]) => setWidth(entry!.contentRect.width))
    observer.observe(node)
    return () => observer.disconnect()
  }, [])
  useEffect(() => {
    if (!chatOpen || !overlay) return
    const previous = document.activeElement
    chatPanel.current?.querySelector<HTMLButtonElement>('button')?.focus()
    return () => {
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus()
    }
  }, [chatOpen, overlay])
  const [mobile, setMobile] = useState(() => window.matchMedia('(max-width: 1023px)').matches)
  useEffect(() => {
    const media = window.matchMedia('(max-width: 1023px)')
    const change = () => setMobile(media.matches)
    media.addEventListener('change', change)
    return () => media.removeEventListener('change', change)
  }, [])
  const { t } = useTranslation()
  const page = useRef<HTMLDivElement>(null)
  const { query, connection } = useRobot()
  const [host, setHost] = useState(rememberedHost)
  const [toast, setToast] = useState<{ text: string; error: boolean } | null>(null)
  const stop = useCallback(async () => {
    // STOP is never debounced: every press reaches the arm, even while one is in flight.
    setToast(null)
    try {
      const result = await rpc.call('robot.stop', {})
      if (!result.ok) throw new Error(result.error || t('robotOperationFailed'))
      setToast({ text: t('robotStopped'), error: false })
    } catch (error) {
      setToast({
        text: `${t('robotStopFailure')}: ${error instanceof Error ? error.message : t('robotOperationFailed')}`,
        error: true,
      })
    }
  }, [t])
  useEffect(() => {
    page.current?.focus({ preventScroll: true })
    const key = (event: KeyboardEvent) => {
      if (event.repeat || event.ctrlKey || event.metaKey || event.altKey) return
      if (event.key !== 'Escape' && event.code !== 'Space') return
      // Chat owns its keyboard shortcuts, including Escape in source/approval dialogs.
      if (chatPanel.current?.contains(document.activeElement)) return
      const active = document.activeElement
      if (active !== document.body && !page.current?.contains(active)) return
      if (
        event.code === 'Space' &&
        active?.closest('input, textarea, button, [role="combobox"], [contenteditable="true"]')
      )
        return
      event.preventDefault()
      void stop()
    }
    window.addEventListener('keydown', key)
    return () => window.removeEventListener('keydown', key)
  }, [stop])
  useEffect(() => {
    if (!toast || toast.error) return
    const timer = setTimeout(() => setToast(null), 5000)
    return () => clearTimeout(timer)
  }, [toast])
  const state = query.data?.state
  const connected = state?.connected ?? false
  const status = connectionStatus(
    query.data,
    connection.isPending && connection.variables !== null,
    query.isError || connection.isError,
  )
  const error =
    connection.error?.message ??
    query.error?.message ??
    query.data?.error ??
    t('robotOperationFailed')
  return (
    <div className="robot-page" ref={page} tabIndex={-1} aria-labelledby="robot-title">
      <header className="chat-header robot-header">
        <PanelToggle
          side="left"
          label="sidebar"
          open={mobile ? shell.drawer : !shell.collapsed}
          onClick={() => shell.toggleSidebar(mobile)}
        />
        <div className="robot-heading">
          <h1 id="robot-title">{t('robot')}</h1>
          <p>{t('robotSubtitle')}</p>
        </div>
        <div className="agent-tool-actions">
          <AgentSwitcher />
          <IconButton
            label={chatOpen ? 'robotCloseChat' : 'robotOpenChat'}
            aria-pressed={chatOpen}
            aria-expanded={chatOpen}
            aria-controls="robot-conversation"
            onClick={() => setChatOpen(!chatOpen)}
          >
            <MessageCircle aria-hidden="true" />
          </IconButton>
        </div>
      </header>
      <div className="robot-layout" ref={split}>
        {chatOpen && (
          <>
            <section
              ref={chatPanel}
              id="robot-conversation"
              className={`chat-column robot-chat ${overlay ? 'robot-chat-overlay' : ''}`}
              aria-label={t('robotChat')}
              style={{ width: dimensions.chat }}
            >
              {overlay && (
                <header className="panel-navigation">
                  <strong>{t('robotChat')}</strong>
                  <IconButton label="robotCloseChat" onClick={() => setChatOpen(false)}>
                    <X aria-hidden="true" />
                  </IconButton>
                </header>
              )}
              <Chat agentId="excalibur" onFile={() => {}} />
            </section>
            {!overlay && (
              <ResizeHandle
                label={t('robotResizeChat')}
                value={dimensions.panel}
                min={320}
                max={Math.max(320, dimensions.available - 360)}
                defaultValue={dimensions.available / 2}
                direction={-1}
                onReset={() => shell.setRatio(0.5)}
                onHome={() => shell.setRatio(0.5)}
                onChange={(value) => {
                  const ratio = splitRatioFromPanel(value, width)
                  if (ratio !== null) shell.setRatio(ratio)
                }}
                getPointerValue={(x) => {
                  const rect = split.current?.getBoundingClientRect()
                  return rect ? rect.right - x : null
                }}
              />
            )}
          </>
        )}
        <div className="robot-workspace" inert={chatOpen && overlay}>
          <div className="robot-grid">
            <section
              className="robot-card robot-connection-card"
              aria-labelledby="robot-connection-title"
            >
              <div className="robot-card-heading">
                <span className="robot-card-icon">
                  <Bot aria-hidden="true" />
                </span>
                <div>
                  <h2 id="robot-connection-title">{t('robotConnection')}</h2>
                  <p>{t('robotConnectionHint')}</p>
                </div>
              </div>
              <div className={`robot-status robot-status-${status}`} role="status">
                <i />
                {t(`robot_${status}`)}
                {status === 'error' ? ` · ${error}` : ''}
              </div>
              <form
                onSubmit={(event) => {
                  event.preventDefault()
                  if (!connection.isPending && (connected || host.trim()))
                    connection.mutate(connected ? null : host.trim())
                }}
              >
                <label htmlFor="robot-host">{t('robotHost')}</label>
                <div className="robot-input">
                  <Network aria-hidden="true" />
                  <input
                    id="robot-host"
                    value={connected ? (state?.host ?? host) : host}
                    placeholder="192.168.1.10"
                    autoComplete="off"
                    autoCapitalize="off"
                    spellCheck={false}
                    aria-describedby="robot-host-hint"
                    disabled={connected || connection.isPending}
                    onChange={(event) => {
                      const next = event.target.value
                      setHost(next)
                      try {
                        localStorage.setItem(hostKey, next)
                      } catch {
                        /* Optional persistence. */
                      }
                    }}
                  />
                </div>
                <p id="robot-host-hint" className="robot-field-hint">
                  {t('robotHostHint')}
                </p>
                <button
                  type="submit"
                  className={`robot-button ${connected ? '' : 'robot-primary'}`}
                  disabled={connection.isPending || (!connected && !host.trim())}
                >
                  {connected ? <Unplug aria-hidden="true" /> : <Plug aria-hidden="true" />}
                  {t(
                    connection.isPending
                      ? connection.variables === null
                        ? 'robotDisconnecting'
                        : 'robot_connecting'
                      : connected
                        ? 'robotDisconnect'
                        : 'robotConnect',
                  )}
                </button>
              </form>
              {connected ? (
                <div className="robot-readouts">
                  <dl>
                    {[
                      [t('robotMode'), state?.robot_mode],
                      [t('robotSafety'), state?.safety_mode],
                      [t('robotGripper'), t(`robot_${state?.gripper ?? 'unknown'}`)],
                    ].map(([label, value]) => (
                      <div key={label}>
                        <dt>{label}</dt>
                        <dd>{value || '—'}</dd>
                      </div>
                    ))}
                  </dl>
                  <span className="robot-tcp-label">{t('robotTCP')}</span>
                  <div className="robot-tcp">
                    {['X', 'Y', 'Z'].map((axis, index) => {
                      const value = state?.tcp_pose?.[index]
                      return (
                        <div key={axis}>
                          <span>{axis}</span>
                          <strong>
                            {typeof value === 'number' && Number.isFinite(value)
                              ? (value * 1000).toFixed(1)
                              : '—'}
                          </strong>
                        </div>
                      )
                    })}
                  </div>
                </div>
              ) : (
                <div className="robot-awaiting">
                  <Bot aria-hidden="true" />
                  <p>{t('robotAwaiting')}</p>
                </div>
              )}
            </section>
            <CameraCard connected={connected} />
          </div>
        </div>
      </div>
      {toast && (
        <div
          className={`robot-toast ${toast.error ? 'robot-error' : ''}`}
          role={toast.error ? 'alert' : 'status'}
        >
          {toast.text}
        </div>
      )}
      <div className="robot-stop-dock">
        <div>
          <strong>{t('robotStopHint')}</strong>
          <span>{t('robotStopKeys')}</span>
        </div>
        <button type="button" className="robot-stop" onClick={() => void stop()}>
          {t('robotStop')}
        </button>
      </div>
    </div>
  )
}

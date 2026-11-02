import { PanelHome, PanelChanges, PanelActivity, usePanelData } from '@/features/panel/Panel'
import { registerPanelOpener } from '@/features/files/open'
import '@/styles/redesign/panel-home.css'
import { AutomationsPage } from '@/features/automations/AutomationsPage'
import { useAutomationEvents } from '@/features/automations/api'
import { loadingLazy as lazy, LoadingFallback } from '@/features/shell/loading'
import { SettingsSkeleton } from '@/features/settings/Skeletons'
import { AgentHeaderSkeleton, WorldSkeleton } from './Skeletons'
import { Whiteboard, WhiteboardConnection } from '@/features/whiteboard/Whiteboard'
import { useWhiteboard } from '@/features/whiteboard/store'
import { conversationId, liveChatEnabled } from '@/features/chat/live'
import { Suspense, useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react'
import { Outlet, useLocation, useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import {
  Swords,
  ClipboardCheck,
  Bell,
  Folder,
  Globe,
  Store,
  Wrench,
  Workflow,
  Settings,
  X,
  LoaderCircle,
} from 'lucide-react'
import { IconButton, PixelAvatar, PageHeader, EmptyState, useLoading } from '@/components/shared'
import { Dialog } from '@/components/ui/dialog'
import { getAgent, getAgents, type AgentId } from './mock-data'
import { validatePanel, type Panel } from './panels'
import { ModeSwitch } from './ModeSwitch'
import { ResizeHandle } from '@/components/resize/ResizeHandle'
import { clampSidebarWidth, sidebarWidthLimit } from './sidebar-size'
import { SidebarRail } from './SidebarRail'
import { ChatHeaderActions } from './ChatHeaderActions'
import { PanelToggle } from '@/components/ui/panel-toggle'
import { ErrorBoundary } from '@/components/ErrorScreen'
import { publishWorldDebug } from '@/features/world/debug'
import { useShell } from './store'
import { splitWidths, splitRatioFromPanel } from './split-layout'
import { ProfileButton, ProfileDialog } from '@/features/profile/Profile'
import { Chat } from '@/features/chat/Chat'
import { useChat } from '@/features/chat/store'
import { Reminders } from '@/features/reminders/Reminders'
import { Tasks } from '@/features/tasks/Tasks'
import { Files } from '@/features/files/Files'
import { Spreadsheet } from '@/features/spreadsheet/Spreadsheet'
import { Projects } from '@/features/projects/Projects'
import { DocumentViewer } from '@/features/viewer/DocumentViewer'
import { useViewerStore } from '@/features/viewer/store'
const SettingsPage = lazy(() => import('@/features/settings/Settings'))
const World = lazy(() => import('@/features/world/World'))
function useMobile() {
  const [mobile, setMobile] = useState(() => window.matchMedia('(max-width: 1023px)').matches)
  useEffect(() => {
    const media = window.matchMedia('(max-width: 1023px)')
    const change = () => setMobile(media.matches)
    media.addEventListener('change', change)
    return () => media.removeEventListener('change', change)
  }, [])
  return mobile
}
export function Shell() {
  const { t } = useTranslation()
  const location = useLocation()
  const navigate = useNavigate()
  const online = useChat((state) => state.online.excalibur ?? false)
  const shell = useShell()
  const viewerTarget = useViewerStore((s) => s.target)
  const mobile = useMobile()
  const boardOpenSequence = useWhiteboard((state) => state.openSequence)
  const activeBoardId = useWhiteboard((state) => (state.open ? state.board?.id : undefined))
  const activeBoardConversation = useWhiteboard((state) =>
    state.open ? state.board?.conversation_id : undefined,
  )
  const [profileOpen, setProfileOpen] = useState(false)
  const [notice, setNotice] = useState<string | null>(null)
  const split = useRef<HTMLDivElement>(null)
  const [splitWidth, setSplitWidth] = useState(1200)
  const sidebar = useRef<HTMLElement>(null)
  const main = useRef<HTMLElement>(null)
  const [navTop, setNavTop] = useState<number | null>(null)
  const [viewportWidth, setViewportWidth] = useState(() => window.innerWidth)
  useEffect(() => {
    const update = () => setViewportWidth(window.innerWidth)
    window.addEventListener('resize', update)
    return () => window.removeEventListener('resize', update)
  }, [])
  const isSettings = location.pathname === '/settings'
  const settingsItem = 'settings'
  const soonPage = location.pathname.split('/')[2] ?? ''
  const soonTitle = ['stores', 'tools'].includes(soonPage) ? soonPage : 'soon'
  const isTasks = location.pathname === '/tasks'
  const isReminders = location.pathname === '/reminders'
  const isAutomations = location.pathname.startsWith('/automations')
  const isWorkPage = isTasks || isReminders || isAutomations
  const onAutomationDeleted = useCallback(
    (id: string) => {
      if (location.pathname === `/automations/${id}`) void navigate({ to: '/automations' })
    },
    [location.pathname, navigate],
  )
  useAutomationEvents(onAutomationDeleted)
  const isProjects = location.pathname === '/projects'
  const isSoon = location.pathname.startsWith('/coming-soon')
  const agentId = location.pathname.split('/')[2] ?? 'excalibur'
  const agent = getAgent(agentId)
  const history = useChat((s) => s.live[agentId])
  const historyError = useChat((s) => s.historyError[agentId])
  const headerLoading = useLoading(liveChatEnabled && !history && !historyError)
  const viewerAgent = useRef(agent.id)
  useEffect(() => {
    if (!activeBoardId || activeBoardConversation !== conversationId(agent.id)) return
    useShell.getState().openPanel()
    void navigate({
      to: '/agents/$agentId',
      params: { agentId: agent.id },
      search: { panel: 'whiteboard', view: 'chat' },
    })
  }, [activeBoardId, activeBoardConversation, boardOpenSequence, agent.id, navigate])

  useLayoutEffect(() => {
    viewerAgent.current = agent.id
  }, [agent.id])
  useEffect(() => {
    if (!viewerTarget) return
    useShell.getState().openPanel()
    void navigate({
      to: '/agents/$agentId',
      params: { agentId: viewerAgent.current },
      search: { panel: 'document', view: 'chat' },
    })
  }, [viewerTarget, navigate])
  const search = validatePanel(location.search as Record<string, unknown>)
  const panel = search.panel
  const worldClock = search.worldClock
  const worldDebug = search.worldDebug,
    worldSpeed = search.worldSpeed
  const isWorld = !isSettings && !isProjects && !isSoon && !isWorkPage && search.view === 'world'
  const dimensions = splitWidths(splitWidth, shell.ratio)
  const overlayPanel = mobile || dimensions.overlay
  const changeMode = useCallback(
    (world: boolean) => {
      try {
        localStorage.setItem('world-last-mode', world ? 'world' : 'chat')
      } catch {
        /* Preferences are optional. */
      }
      void navigate({
        to: '/agents/$agentId',
        params: { agentId: agent.id },
        search: { panel, worldDebug, worldSpeed, worldClock, view: world ? 'world' : 'chat' },
      })
    },
    [navigate, agent.id, panel, worldDebug, worldSpeed, worldClock],
  )
  useEffect(() => {
    if (!isWorld) publishWorldDebug({ running: false, fps: 0, mode: 'chat', ready: false })
    if (search.view) {
      try {
        localStorage.setItem('world-last-mode', search.view)
      } catch {
        /* Preferences are optional. */
      }
    }
  }, [search.view, isWorld])
  const setWorldRoom = useCallback(
    (room?: string) => {
      void navigate({
        to: '/agents/$agentId',
        params: { agentId: agent.id },
        search: { panel, worldDebug, worldSpeed, worldClock, view: 'world', room },
      })
    },
    [navigate, agent.id, panel, worldDebug, worldSpeed, worldClock],
  )
  useEffect(() => {
    if (search.view || isSettings || isProjects || isSoon || isWorkPage) return
    try {
      if (localStorage.getItem('world-last-mode') === 'world') changeMode(true)
    } catch {
      /* Preferences are optional. */
    }
  }, [search.view, isSettings, isProjects, isSoon, isWorkPage, changeMode])
  useEffect(() => {
    const key = (event: KeyboardEvent) => {
      if ((event.ctrlKey || event.metaKey) && event.key.toLowerCase() === 'j') {
        event.preventDefault()
        changeMode(!isWorld)
      }
    }
    window.addEventListener('keydown', key)
    return () => window.removeEventListener('keydown', key)
  }, [isWorld, changeMode])
  const setPanel = useCallback(
    (value: Panel) => {
      void navigate({
        to: '/agents/$agentId',
        params: { agentId: agent.id },
        search: { panel: value, view: 'chat' },
      })
      useShell.getState().openPanel()
    },
    [navigate, agent.id],
  )
  useEffect(() => {
    registerPanelOpener(setPanel)
    return () => registerPanelOpener(null)
  }, [setPanel])
  const panelData = usePanelData(agent.id)
  const go = (path: string) => {
    shell.setDrawer(false)
    if (path === 'settings' || path === 'metrics')
      void navigate({
        to: '/settings',
        search: { section: path === 'metrics' ? 'usage' : 'general' },
      })
    else if (path === 'myTasks' || path === '/tasks') void navigate({ to: '/tasks' })
    else if (path === 'reminders' || path === '/reminders') void navigate({ to: '/reminders' })
    else if (path === 'automations') void navigate({ to: '/automations' })
    else if (path === '/projects') void navigate({ to: '/projects' })
    else void navigate({ to: '/coming-soon/$page', params: { page: path } })
  }
  useEffect(() => {
    const handler = (event: KeyboardEvent) => {
      if ((event.metaKey || event.ctrlKey) && event.key.toLowerCase() === 'b') {
        event.preventDefault()
        if (isWorld && !mobile) {
          if (shell.collapsed) shell.toggleSidebar(false)
          changeMode(false)
        } else shell.toggleSidebar(mobile)
      }
      if ((event.metaKey || event.ctrlKey) && event.key === '.') {
        event.preventDefault()
        shell.togglePanel()
      }
      if (event.key === 'Escape') {
        if (document.querySelector('dialog[open]')) return
        shell.setDrawer(false)
        setNotice(null)
        if (overlayPanel && shell.panelOpen) shell.togglePanel()
      }
    }
    window.addEventListener('keydown', handler)
    return () => window.removeEventListener('keydown', handler)
  }, [shell, mobile, overlayPanel, isWorld, changeMode])
  useLayoutEffect(() => {
    const node = split.current
    if (!node) return
    const measure = () => {
      if (
        split.current !== node ||
        !node.isConnected ||
        node.getClientRects().length === 0 ||
        getComputedStyle(node).visibility !== 'visible'
      )
        return
      const width = node.getBoundingClientRect().width
      if (Number.isFinite(width) && width > 0) setSplitWidth(width)
    }
    measure()
    const observer = new ResizeObserver(measure)
    observer.observe(node)
    return () => observer.disconnect()
  }, [isSettings, isProjects, isSoon, isWorld, isWorkPage])
  useEffect(() => {
    if (!overlayPanel || (!shell.drawer && !shell.panelOpen)) return
    const previous = document.activeElement
    const selector = shell.drawer ? '.sidebar button' : '.overlay-panel [aria-selected="true"]'
    document.querySelector<HTMLElement>(selector)?.focus()
    return () => {
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus()
    }
  }, [overlayPanel, shell.drawer, shell.panelOpen])
  const sidebarVisible = mobile ? shell.drawer : !isWorld && !shell.collapsed
  const railVisible = !mobile && (isWorld || shell.collapsed)
  const sidebarWidth = clampSidebarWidth(shell.sidebarWidth, viewportWidth)
  useLayoutEffect(() => {
    const node = sidebar.current
    if (!node || !sidebarVisible) return
    const update = () => {
      const selected = node.querySelector<HTMLElement>('nav button.selected')
      setNavTop(selected ? selected.offsetTop : null)
    }
    update()
    const observer = new ResizeObserver(update)
    observer.observe(node)
    return () => observer.disconnect()
  }, [location.pathname, location.search, sidebarVisible])
  useEffect(() => {
    if (window.matchMedia('(prefers-reduced-motion: reduce)').matches) return
    const node = main.current
    if (!node) return
    const tokens = getComputedStyle(node)
    const animation = node.animate(
      [
        { opacity: 0, transform: 'translateY(6px)' },
        { opacity: 1, transform: 'translateY(0)' },
      ],
      {
        duration: parseFloat(tokens.getPropertyValue('--dur-base')),
        easing: tokens.getPropertyValue('--ease-out').trim(),
      },
    )
    return () => animation?.cancel()
  }, [location.pathname, isWorld])
  const availableWidth = dimensions.available
  const panelWidth = dimensions.panel
  const resize = (width: number) => {
    const node = split.current
    if (
      !node?.isConnected ||
      node.getClientRects().length === 0 ||
      getComputedStyle(node).visibility !== 'visible'
    )
      return
    const ratio = splitRatioFromPanel(width, node.getBoundingClientRect().width)
    if (ratio !== null) shell.setRatio(ratio)
  }
  const selectAgent = (id: AgentId) => {
    shell.setDrawer(false)
    void navigate({
      to: '/agents/$agentId',
      params: { agentId: id },
      search: {
        panel: isWorld ? panel : 'files',
        worldDebug,
        worldSpeed,
        worldClock,
        view: isWorld ? 'world' : 'chat',
      },
    })
  }
  return (
    <div
      className={`app ${sidebarVisible ? 'sidebar-visible' : 'sidebar-collapsed'} ${isWorld ? 'world-mode' : ''}`}
    >
      {mobile && shell.drawer && (
        <button
          className="drawer-backdrop"
          aria-label={t('close')}
          onClick={() => shell.setDrawer(false)}
        />
      )}
      <div
        className={`sidebar-slot ${railVisible ? 'sidebar-slot-rail' : ''}`}
        style={{ width: mobile ? undefined : railVisible ? 64 : sidebarWidth + 1 }}
      >
        {railVisible && (
          <SidebarRail
            agents={getAgents()}
            selectedAgentId={
              !isSettings && !isProjects && !isSoon && !isWorkPage ? agent.id : undefined
            }
            activeItem={
              isSettings
                ? settingsItem
                : isWorld
                  ? 'world'
                  : isProjects
                    ? 'projects'
                    : isWorkPage
                      ? isAutomations
                        ? 'automations'
                        : isTasks
                          ? 'myTasks'
                          : 'reminders'
                      : isSoon
                        ? location.pathname.split('/')[2]
                        : undefined
            }
            onNavigate={(key) =>
              key === 'world' ? changeMode(true) : go(key === 'projects' ? '/projects' : key)
            }
            onAgent={selectAgent}
            onProfile={() => setProfileOpen(true)}
          />
        )}
        <aside
          ref={sidebar}
          style={{ width: sidebarVisible && !mobile ? sidebarWidth : undefined }}
          className={`sidebar ${sidebarVisible ? '' : 'sidebar-hidden'}`}
          aria-label={t('workspace')}
          inert={!sidebarVisible || (mobile && shell.panelOpen)}
          onKeyDown={(event) => {
            if (mobile && event.key === 'Tab') {
              const nodes = event.currentTarget.querySelectorAll<HTMLElement>('button, input, a')
              const first = nodes[0],
                last = nodes[nodes.length - 1]
              if (event.shiftKey && document.activeElement === first) {
                event.preventDefault()
                last?.focus()
              } else if (!event.shiftKey && document.activeElement === last) {
                event.preventDefault()
                first?.focus()
              }
            }
          }}
        >
          <span
            className="sidebar-active-bar"
            aria-hidden="true"
            style={{ transform: `translateY(${navTop ?? 0}px)`, opacity: navTop === null ? 0 : 1 }}
          />
          <div className="workspace-row">
            <Swords className="workspace-logo" />
            <strong>{t('workspace')}</strong>
          </div>
          <div className="sidebar-section-title">{t('agents')}</div>
          <nav className="agent-nav">
            {getAgents().map((a) => (
              <button
                key={a.id}
                className={
                  !isSettings && !isProjects && !isSoon && !isWorkPage && agent.id === a.id
                    ? 'selected'
                    : ''
                }
                aria-current={
                  !isSettings && !isProjects && !isSoon && !isWorkPage && agent.id === a.id
                    ? 'page'
                    : undefined
                }
                onClick={() => selectAgent(a.id)}
              >
                <PixelAvatar id={a.id} />
                <span className="sidebar-label">{t(a.name, { defaultValue: a.name })}</span>
                {online && <i className="online-dot" />}
                {a.working ? (
                  <LoaderCircle className="working-spinner" aria-label={t('working')} />
                ) : a.unread ? (
                  <span className="unread">{a.unread}</span>
                ) : null}
              </button>
            ))}
          </nav>
          <nav className="primary-nav">
            {[
              { key: 'myTasks', icon: ClipboardCheck },
              { key: 'reminders', icon: Bell },
              { key: 'projects', icon: Folder },
              { key: 'world', icon: Globe },
            ].map(({ key, icon: Icon }) => (
              <button
                key={key}
                className={
                  (isProjects && key === 'projects') ||
                  (isTasks && key === 'myTasks') ||
                  (isReminders && key === 'reminders')
                    ? 'selected'
                    : ''
                }
                aria-current={
                  (isProjects && key === 'projects') ||
                  (isTasks && key === 'myTasks') ||
                  (isReminders && key === 'reminders')
                    ? 'page'
                    : undefined
                }
                onClick={() =>
                  key === 'world' ? changeMode(true) : go(key === 'projects' ? '/projects' : key)
                }
              >
                <Icon />
                <span className="sidebar-label">{t(key)}</span>
              </button>
            ))}
          </nav>
          <nav className="secondary-nav">
            {[
              { key: 'stores', icon: Store },
              { key: 'tools', icon: Wrench },
              { key: 'automations', icon: Workflow },
            ].map(({ key, icon: Icon }) => (
              <button
                key={key}
                className={
                  (
                    key === 'automations'
                      ? isAutomations
                      : location.pathname === `/coming-soon/${key}`
                  )
                    ? 'selected'
                    : ''
                }
                aria-current={
                  (
                    key === 'automations'
                      ? isAutomations
                      : location.pathname === `/coming-soon/${key}`
                  )
                    ? 'page'
                    : undefined
                }
                onClick={() => go(key)}
              >
                <Icon />
                <span className="sidebar-label">{t(key)}</span>
              </button>
            ))}
          </nav>
          <div className="sidebar-bottom">
            <nav>
              {[{ key: 'settings', icon: Settings }].map(({ key, icon: Icon }) => (
                <button
                  key={key}
                  className={isSettings && key === settingsItem ? 'selected' : ''}
                  aria-current={isSettings && key === settingsItem ? 'page' : undefined}
                  onClick={() => go(key)}
                >
                  <Icon />
                  <span className="sidebar-label">{t(key)}</span>
                </button>
              ))}
            </nav>
            <div className="user-row">
              <ProfileButton onClick={() => setProfileOpen(true)} />
            </div>
          </div>
        </aside>
        {sidebarVisible && !mobile && (
          <ResizeHandle
            label={t('resizeSidebar')}
            className="sidebar-resize-handle"
            value={sidebarWidth}
            min={200}
            max={sidebarWidthLimit(viewportWidth)}
            defaultValue={240}
            onChange={(width) => shell.setSidebarWidth(width, viewportWidth)}
            getPointerValue={(x) => {
              const node = sidebar.current
              if (!node?.isConnected || node.getClientRects().length === 0) return null
              const rect = node.getBoundingClientRect()
              return rect.width > 0 ? x - rect.left : null
            }}
          />
        )}
      </div>
      <main className="main-panel" ref={main}>
        <ErrorBoundary resetKey={location.pathname + location.searchStr}>
          {isWorld && mobile && (
            <div className="world-sidebar-toggle">
              <PanelToggle
                side="left"
                label="sidebar"
                open={shell.drawer}
                onClick={() => shell.setDrawer(true)}
              />
            </div>
          )}
          {isWorld && (
            <ModeSwitch
              variant="pill"
              mode={isWorld ? 'world' : 'chat'}
              onChange={(mode) => changeMode(mode === 'world')}
            />
          )}
          {(isSettings || isProjects || isSoon || isWorkPage) && (
            <div className="page-sidebar-toggle">
              <PanelToggle
                side="left"
                label="sidebar"
                open={mobile ? shell.drawer : !shell.collapsed}
                onClick={() => shell.toggleSidebar(mobile)}
              />
            </div>
          )}
          {isSettings ? (
            <Suspense
              fallback={
                <LoadingFallback>
                  <SettingsSkeleton />
                </LoadingFallback>
              }
            >
              <SettingsPage />
            </Suspense>
          ) : isWorld ? (
            <Suspense
              fallback={
                <LoadingFallback>
                  <WorldSkeleton />
                </LoadingFallback>
              }
            >
              <World
                roomId={search.room}
                agentId={agent.id}
                onRoom={setWorldRoom}
                onOpen={(id, nextPanel) => {
                  shell.openPanel()
                  try {
                    localStorage.setItem('world-last-mode', 'chat')
                  } catch {
                    /* Preferences are optional. */
                  }
                  void navigate({
                    to: '/agents/$agentId',
                    params: { agentId: id },
                    search: { panel: nextPanel, worldDebug, worldSpeed, worldClock, view: 'chat' },
                  })
                }}
              />
            </Suspense>
          ) : isAutomations ? (
            location.pathname === '/automations' ? (
              <AutomationsPage />
            ) : (
              <Outlet />
            )
          ) : isTasks ? (
            <Tasks page />
          ) : isReminders ? (
            <Reminders />
          ) : isProjects ? (
            <Projects onNotice={setNotice} />
          ) : isSoon ? (
            <div className="coming-soon-page">
              <PageHeader eyebrow="workspace" title={soonTitle} subtitle="soonDetail" />
              <EmptyState
                icon={
                  location.pathname.endsWith('stores')
                    ? Store
                    : location.pathname.endsWith('tools')
                      ? Wrench
                      : Workflow
                }
                title="soon"
                description="soonDetail"
              />
            </div>
          ) : (
            <div className="split-layout" ref={split}>
              <section
                className="chat-column"
                inert={overlayPanel && shell.panelOpen}
                style={{
                  width: !overlayPanel && shell.panelOpen ? `${dimensions.chat}px` : '100%',
                }}
              >
                <header className="chat-header">
                  <PanelToggle
                    side="left"
                    label="sidebar"
                    open={mobile ? shell.drawer : !shell.collapsed}
                    onClick={() => shell.toggleSidebar(mobile)}
                  />
                  {headerLoading.visible && <AgentHeaderSkeleton />}
                  {!headerLoading.busy && (
                    <>
                      <PixelAvatar id={agent.id} size={40} />
                      <div className="agent-heading">
                        <h1>{t(agent.name, { defaultValue: agent.name })}</h1>
                        <p>{t(agent.role)}</p>
                      </div>
                    </>
                  )}
                  <ChatHeaderActions
                    onMode={(mode) => changeMode(mode === 'world')}
                    onNotice={setNotice}
                    onPanel={shell.togglePanel}
                    panelOpen={shell.panelOpen}
                  />
                </header>
                <Chat key={agent.id} agentId={agent.id} onFile={() => {}} />
              </section>
              {shell.panelOpen && (
                <>
                  {!overlayPanel && (
                    <ResizeHandle
                      label={t('resize')}
                      value={panelWidth}
                      min={320}
                      max={Math.max(320, availableWidth - 360)}
                      defaultValue={availableWidth / 2}
                      direction={-1}
                      onReset={() => shell.setRatio(0.5)}
                      onHome={() => shell.setRatio(0.5)}
                      onChange={resize}
                      getPointerValue={(x) => {
                        const rect = split.current?.getBoundingClientRect()
                        return rect && rect.width > 0 ? rect.right - x : null
                      }}
                    />
                  )}
                  <section
                    className={`right-panel ${overlayPanel ? 'overlay-panel' : ''} ${mobile ? 'mobile-panel' : ''}`}
                    style={{ width: overlayPanel ? '100%' : panelWidth }}
                    aria-label={t(panel === 'changes' ? 'panelChangesTitle' : panel)}
                  >
                    <header className="panel-navigation">
                      {panel !== 'home' && (
                        <button onClick={() => setPanel('home')} aria-label={t('panelBack')}>
                          ← {t('panelHome')}
                        </button>
                      )}
                      {panel !== 'home' && (
                        <strong>{t(panel === 'changes' ? 'panelChangesTitle' : panel)}</strong>
                      )}
                      <IconButton label="close" onClick={shell.togglePanel}>
                        <X />
                      </IconButton>
                    </header>
                    <div
                      className={`panel-content panel-slide ${panel === 'home' ? 'panel-slide-back' : ''}`}
                      key={panel}
                      id={`panel-${panel}`}
                      role="region"
                      aria-label={t(panel === 'changes' ? 'panelChangesTitle' : panel)}
                    >
                      {panel === 'home' ? (
                        <PanelHome data={panelData} onView={setPanel} />
                      ) : panel === 'activity' ? (
                        <PanelActivity data={panelData} />
                      ) : panel === 'changes' ? (
                        <PanelChanges changes={panelData.changes} />
                      ) : panel === 'whiteboard' ? (
                        <Whiteboard conversationId={conversationId(agent.id)} />
                      ) : panel === 'files' ? (
                        <Files changes={panelData.changes} />
                      ) : panel === 'document' ? (
                        <DocumentViewer onClose={() => useShell.getState().togglePanel()} />
                      ) : panel === 'sheet' ? (
                        <Spreadsheet />
                      ) : (
                        <div className="empty-state">
                          <Folder />
                          <h2>{t('soon')}</h2>
                          <p>{t('soonDetail')}</p>
                        </div>
                      )}
                    </div>
                  </section>
                </>
              )}
            </div>
          )}
        </ErrorBoundary>
      </main>
      <WhiteboardConnection />
      {profileOpen && <ProfileDialog onClose={() => setProfileOpen(false)} />}
      {notice && (
        <Dialog title={notice} onClose={() => setNotice(null)}>
          <p>{t(notice === 'info' ? 'infoDetail' : 'soonDetail')}</p>
        </Dialog>
      )}
    </div>
  )
}

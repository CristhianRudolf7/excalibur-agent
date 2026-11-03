import { AgentSwitcher } from '@/features/agents/AgentSwitcher'
import { useSelectedAgent } from '@/features/agents/store'
import { QuizSession } from './QuizSession'
import { TopicPage } from './TopicPage'
import { CoursePage } from './CoursePage'
import { useClassroom } from './store'
import { useCourses, useCourse, useClassroomEvents } from './api'
import { Courses } from './Courses'
import { Button } from '@/components/ui/button'
import { useEffect, useRef, useState } from 'react'
import { ResizeHandle } from '@/components/resize/ResizeHandle'
import { useTranslation } from 'react-i18next'
import { GraduationCap, MessageCircle, X } from 'lucide-react'
import { IconButton } from '@/components/shared'
import { PanelToggle } from '@/components/ui/panel-toggle'
import { Chat } from '@/features/chat/Chat'
import { useShell } from '@/features/shell/store'
import './classroom.css'
export function Classroom() {
  const { t } = useTranslation()
  const agent = useSelectedAgent()
  useClassroomEvents()
  const state = useClassroom()
  const courses = useCourses()
  const course = useCourse(state.courseId)
  const context = {
    app: 'classroom',
    ...(state.courseId ? { course_id: state.courseId } : {}),
    ...(state.topicId ? { topic_id: state.topicId } : {}),
  } as const
  const [creating, setCreating] = useState(false)
  const empty = !state.courseId && (creating || courses.data?.courses.length === 0)
  const shell = useShell()
  const [chatOpen, setChatOpen] = useState(false)
  const [mobile, setMobile] = useState(() => window.matchMedia('(max-width: 1023px)').matches)
  const [chatWidth, setChatWidth] = useState(420)
  const [layoutWidth, setLayoutWidth] = useState(1000)
  const layout = useRef<HTMLDivElement>(null)
  const chatPanel = useRef<HTMLElement>(null)
  useEffect(() => {
    const node = layout.current
    if (!node || typeof ResizeObserver === 'undefined') return
    const observer = new ResizeObserver(() => setLayoutWidth(node.clientWidth))
    observer.observe(node)
    return () => observer.disconnect()
  }, [])
  useEffect(() => {
    const media = window.matchMedia('(max-width: 1023px)')
    const change = () => setMobile(media.matches)
    media.addEventListener('change', change)
    return () => media.removeEventListener('change', change)
  }, [])
  useEffect(() => {
    if (!chatOpen) return
    const previous = document.activeElement as HTMLElement | null
    const panel = chatPanel.current
    panel?.querySelector<HTMLElement>('button')?.focus()
    const key = (event: KeyboardEvent) => {
      if (event.key === 'Escape') {
        event.preventDefault()
        setChatOpen(false)
      }
      if (!mobile || event.key !== 'Tab') return
      const controls = Array.from(
        panel?.querySelectorAll<HTMLElement>(
          'button:not(:disabled), input:not(:disabled), textarea:not(:disabled), select:not(:disabled), [tabindex="0"]',
        ) ?? [],
      ).filter((node) => node.getClientRects().length > 0)
      const first = controls[0],
        last = controls.at(-1)
      if (event.shiftKey && document.activeElement === first) {
        event.preventDefault()
        last?.focus()
      } else if (!event.shiftKey && document.activeElement === last) {
        event.preventDefault()
        first?.focus()
      }
    }
    document.addEventListener('keydown', key)
    return () => {
      document.removeEventListener('keydown', key)
      previous?.focus()
    }
  }, [chatOpen, mobile])
  return (
    <div className="classroom-page">
      <header className="chat-header">
        <PanelToggle
          side="left"
          label="sidebar"
          open={mobile ? shell.drawer : !shell.collapsed}
          onClick={() => shell.toggleSidebar(mobile)}
        />
        <GraduationCap aria-hidden="true" />
        <div className="agent-heading">
          <h1>{t('classroom')}</h1>
        </div>
        <div className="header-actions">
          <AgentSwitcher />
          <IconButton
            label="classChat"
            aria-expanded={chatOpen}
            aria-controls="classroom-conversation"
            onClick={() => setChatOpen(!chatOpen)}
          >
            <MessageCircle />
          </IconButton>
        </div>
      </header>
      <div className="classroom-layout" ref={layout}>
        <section className="classroom-content" inert={mobile && chatOpen}>
          {state.courseId ? (
            <>
              <Button
                onClick={() => {
                  state.select()
                  setCreating(false)
                }}
              >
                {t('classBack')}
              </Button>
              {course.isPending && <p role="status">{t('classLoading')}</p>}
              {course.isError && (
                <p role="alert">
                  {t('classError')}{' '}
                  <Button onClick={() => void course.refetch()}>{t('classRetry')}</Button>
                </p>
              )}
              {course.data && state.quizOpen ? (
                <QuizSession
                  key={`${course.data.id}:${state.topicId ?? 'final'}`}
                  course={course.data}
                  topicId={state.topicId}
                  onBack={() => useClassroom.setState({ quizOpen: false })}
                />
              ) : course.data && state.topicId ? (
                <TopicPage
                  key={state.topicId}
                  course={course.data}
                  topicId={state.topicId}
                  onBack={() => state.select(course.data.id)}
                  onQuiz={() => useClassroom.setState({ quizOpen: true })}
                  onChat={() => setChatOpen(true)}
                />
              ) : (
                course.data && (
                  <CoursePage
                    course={course.data}
                    onTopic={(id) => state.select(course.data.id, id)}
                    onExam={() => useClassroom.setState({ quizOpen: true })}
                  />
                )
              )}
            </>
          ) : (
            <>
              {courses.isPending && <p role="status">{t('classLoading')}</p>}
              {courses.isError && (
                <p role="alert">
                  {t('classError')}{' '}
                  <Button onClick={() => void courses.refetch()}>{t('classRetry')}</Button>
                </p>
              )}
              {!empty && courses.data && (
                <>
                  <div className="classroom-actions">
                    <h2>{t('classCourses')}</h2>
                    <Button onClick={() => setCreating(true)}>{t('classNew')}</Button>
                  </div>
                  <Courses courses={courses.data.courses} onOpen={state.select} />
                </>
              )}
              {empty && (
                <>
                  <h2>{t('classStudy')}</h2>
                  <p>{t('classStudyHint')}</p>
                  {creating && <Button onClick={() => setCreating(false)}>{t('classBack')}</Button>}
                  {!chatOpen && (
                    <Chat
                      key={agent.id}
                      agentId={agent.id}
                      onFile={() => {}}
                      context={context}
                      showWelcome={false}
                    />
                  )}
                </>
              )}
            </>
          )}
        </section>
        {chatOpen && (
          <>
            {!mobile && (
              <ResizeHandle
                label={t('classResizeChat')}
                value={chatWidth}
                min={320}
                max={Math.max(320, layoutWidth * 0.6)}
                defaultValue={420}
                direction={-1}
                onChange={setChatWidth}
                getPointerValue={(x) => {
                  const rect = layout.current?.getBoundingClientRect()
                  return rect && rect.width > 0 ? rect.right - x : null
                }}
              />
            )}
            <section
              ref={chatPanel}
              id="classroom-conversation"
              className="right-panel classroom-chat"
              style={mobile ? undefined : { width: chatWidth }}
              aria-label={t('classChat')}
              role={mobile ? 'dialog' : undefined}
              aria-modal={mobile || undefined}
            >
              <header className="panel-navigation">
                <strong>{t('classChat')}</strong>
                <IconButton label="close" onClick={() => setChatOpen(false)}>
                  <X />
                </IconButton>
              </header>
              <Chat
                key={agent.id}
                agentId={agent.id}
                onFile={() => {}}
                context={context}
                showWelcome={false}
              />
            </section>
          </>
        )}
      </div>
    </div>
  )
}

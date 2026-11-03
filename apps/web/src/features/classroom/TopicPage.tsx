import { useSelectedAgent } from '@/features/agents/store'
import { useEffect, useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Markdown } from '@/components/markdown/Markdown'
import { Whiteboard } from '@/features/whiteboard/Whiteboard'
import { conversationId, sendMessage } from '@/features/chat/live'
import { rpc } from '@/lib/rpc/client'
import type { Course } from '@/lib/rpc/classroom'
import { targetKey, useClassroom } from './store'
import { classroomKey } from './api'
export function TopicPage({
  course,
  topicId,
  onBack,
  onQuiz,
  onChat,
}: {
  course: Course
  topicId: string
  onBack: () => void
  onQuiz: () => void
  onChat: () => void
}) {
  const { t } = useTranslation()
  const agent = useSelectedAgent()
  const client = useQueryClient()
  const topic = course.units.flatMap((unit) => unit.topics).find((topic) => topic.id === topicId)
  const preparing = useClassroom((state) => state.preparing[targetKey(course.id, topicId)])
  const opened = useQuery({
    queryKey: ['classroom-topic-open', course.id, topicId],
    queryFn: () => rpc.call('classroom.open_topic', { course_id: course.id, topic_id: topicId }),
    retry: false,
    refetchOnWindowFocus: false,
    staleTime: Infinity,
    refetchOnMount: 'always',
    enabled: Boolean(topic),
  })
  useEffect(() => {
    if (opened.data)
      void client.invalidateQueries({ queryKey: [...classroomKey, 'course', course.id] })
  }, [opened.data, client, course.id])
  const [regenerating, setRegenerating] = useState(false)
  const [regenerationError, setRegenerationError] = useState(false)
  // Course snapshots refresh on class_ready; a newer stored lesson replaces open_topic's response.
  const lesson = topic?.class ?? opened.data?.class ?? opened.data?.topic.class
  if (!topic)
    return (
      <p role="alert">
        {t('classTopicMissing')}
        <Button onClick={onBack}>{t('classSyllabus')}</Button>
      </p>
    )
  const regenerate = async () => {
    setRegenerating(true)
    setRegenerationError(false)
    const sent = await sendMessage(
      agent.id,
      t('classRegenerateRequest', { topic: topic.title, lng: course.language }),
      [],
      undefined,
      undefined,
      { app: 'classroom', course_id: course.id, topic_id: topicId },
    )
    setRegenerating(false)
    setRegenerationError(!sent)
    onChat()
  }
  return (
    <>
      <Button onClick={onBack}>{t('classSyllabus')}</Button>
      <div className="classroom-actions">
        <h2>{topic.title}</h2>
        <span>{t(`classStatus_${topic.status}`)}</span>
      </div>
      <p>{topic.summary}</p>
      {opened.isError && (
        <p role="alert">
          {t('classError')} <Button onClick={() => void opened.refetch()}>{t('classRetry')}</Button>
        </p>
      )}
      {(opened.isPending || preparing || (!lesson && !opened.isError)) && (
        <div className="classroom-card" role="status" aria-live="polite">
          <progress aria-label={t('classPreparing')} />
          <p>{t('classPreparing')}</p>
        </div>
      )}
      {lesson && (
        <>
          {lesson.format === 'whiteboard' && lesson.whiteboard_id && (
            <div className="classroom-whiteboard">
              <Whiteboard
                conversationId={conversationId(agent.id)}
                boardId={lesson.whiteboard_id}
              />
            </div>
          )}
          <article className="classroom-lesson">
            <Markdown>{lesson.content}</Markdown>
          </article>
          <div className="classroom-actions">
            <Button variant="primary" onClick={onQuiz}>
              {t('classStartQuiz')}
            </Button>
            <Button disabled={regenerating || preparing} onClick={() => void regenerate()}>
              {t('classRegenerate')}
            </Button>
          </div>
        </>
      )}
      {regenerationError && <p role="alert">{t('classError')}</p>}
    </>
  )
}

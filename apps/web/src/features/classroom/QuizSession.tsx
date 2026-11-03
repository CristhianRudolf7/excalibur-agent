import { useCallback, useEffect, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Markdown } from '@/components/markdown/Markdown'
import { rpc } from '@/lib/rpc/client'
import type { Course, Grade } from '@/lib/rpc/classroom'
import { useClassroom, targetKey } from './store'
import { classroomKey } from './api'
import { examUnlocked } from './logic'
import { Quiz } from './Quiz'
export function QuizSession({
  course,
  topicId,
  onBack,
}: {
  course: Course
  topicId?: string
  onBack: () => void
}) {
  const { t, i18n } = useTranslation()
  const client = useQueryClient()
  const key = targetKey(course.id, topicId)
  const quizId = useClassroom((state) => state.quizzes[key])
  const requesting = useClassroom((state) => state.requesting[key])
  const [grade, setGrade] = useState<Grade>()
  const [waiting, setWaiting] = useState(false)
  const target = { course_id: course.id, ...(topicId ? { topic_id: topicId } : { final: true }) }
  const allowed = Boolean(topicId) || examUnlocked(course)
  const request = useMutation({
    mutationFn: () => rpc.call('classroom.request_quiz', target),
    onMutate: () => {
      setGrade(undefined)
      setWaiting(true)
      useClassroom.setState((state) => ({ requesting: { ...state.requesting, [key]: true } }))
      useClassroom.setState((state) => ({ quizzes: { ...state.quizzes, [key]: undefined } }))
    },
    onSuccess: (result) => {
      const ready = result.quiz
      if (ready)
        useClassroom.setState((state) => ({
          quizzes: { ...state.quizzes, [key]: ready.id },
          requesting: { ...state.requesting, [key]: false },
        }))
    },
    onError: () => {
      setWaiting(false)
      useClassroom.setState((state) => ({ requesting: { ...state.requesting, [key]: false } }))
    },
  })
  const started = useRef(false)
  const startRequest = request.mutate
  useEffect(() => {
    if (started.current || !allowed) return
    started.current = true
    const state = useClassroom.getState()
    if (!state.requesting[key]) startRequest()
  }, [allowed, key, startRequest])
  const quiz = useQuery({
    queryKey: [...classroomKey, 'quiz', quizId],
    queryFn: () => rpc.call('classroom.get_quiz', { quiz_id: quizId! }),
    enabled: Boolean(quizId) && allowed,
  })
  const attempts = useQuery({
    queryKey: [...classroomKey, 'attempts', course.id, topicId],
    queryFn: () =>
      rpc.call('classroom.attempts', {
        course_id: course.id,
        ...(topicId ? { topic_id: topicId } : {}),
      }),
  })
  const onGraded = useCallback(
    (grade: Grade) => {
      setGrade(grade)
      void client.invalidateQueries({ queryKey: classroomKey })
    },
    [client],
  )
  // A ready quiz belongs to this target; stale events from another topic never select it.
  const currentQuiz =
    quiz.data &&
    quiz.data.course_id === course.id &&
    (topicId ? quiz.data.topic_id === topicId && !quiz.data.final : quiz.data.final)
      ? quiz.data
      : undefined
  const currentAttempt = attempts.data?.attempts.find((attempt) =>
    grade ? attempt.id === grade.attempt : attempt.quiz_id === quizId,
  )
  return (
    <>
      <Button onClick={onBack}>{t('classSyllabus')}</Button>
      <h2>{t(topicId ? 'classQuiz' : 'classFinal')}</h2>
      {!allowed ? (
        <p>{t('classFinalLocked')}</p>
      ) : (
        <>
          {!currentQuiz && !quiz.isError && !waiting && !requesting && !quizId && (
            <Button variant="primary" onClick={() => request.mutate()}>
              {t(topicId ? 'classStartQuiz' : 'classStartExam')}
            </Button>
          )}
          {!currentQuiz &&
            !quiz.isError &&
            (request.isPending || requesting || waiting || (quizId && quiz.isPending)) && (
              <div role="status">
                <progress aria-label={t('classQuizPreparing')} />
                <p>{t('classQuizPreparing')}</p>
              </div>
            )}
          {request.isError && (
            <div role="alert">
              {t('classError')}
              <Button onClick={() => request.mutate()}>{t('classRetry')}</Button>
            </div>
          )}
          {quiz.isError && (
            <div role="alert">
              {t('classError')}
              <Button onClick={() => void quiz.refetch()}>{t('classRetry')}</Button>
            </div>
          )}
          {currentQuiz && (
            <Quiz
              key={currentQuiz.id}
              quiz={currentQuiz}
              attempt={currentAttempt}
              onGraded={onGraded}
              onRetry={() => request.mutate()}
            />
          )}
        </>
      )}
      {attempts.isError && (
        <p role="alert">
          {t('classError')}
          <Button onClick={() => void attempts.refetch()}>{t('classRetry')}</Button>
        </p>
      )}
      {!!attempts.data?.attempts.length && (
        <details>
          <summary>{t('classAttempts')}</summary>
          {attempts.data.attempts.map((attempt) => (
            <article className="classroom-card" key={attempt.id}>
              <strong>{t(attempt.passed ? 'classPassed' : 'classNotPassed')}</strong>
              <time dateTime={attempt.created_at}>
                {new Date(attempt.created_at).toLocaleString(i18n.language)}
              </time>
              {attempt.feedback && <Markdown>{attempt.feedback}</Markdown>}
            </article>
          ))}
        </details>
      )}
    </>
  )
}

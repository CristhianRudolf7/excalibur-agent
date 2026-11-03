import { useState } from 'react'
import { useMutation } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Markdown } from '@/components/markdown/Markdown'
import { rpc } from '@/lib/rpc/client'
import type { Attempt, Grade, QuizView } from '@/lib/rpc/classroom'
import { buildAnswers, toggleOption, type AnswerDraft } from './logic'
export function Quiz({
  quiz,
  attempt,
  onGraded,
  onRetry,
}: {
  quiz: QuizView
  attempt?: Attempt
  onGraded: (grade: Grade) => void
  onRetry: () => void
}) {
  const { t } = useTranslation()
  const [draft, setDraft] = useState<AnswerDraft>({})
  const submission = useMutation({
    mutationFn: async () => {
      const answers = buildAnswers(quiz.questions, draft)
      if (!answers) throw new Error('Incomplete answers')
      return rpc.call('classroom.submit', { quiz_id: quiz.id, answers })
    },
    onSuccess: onGraded,
  })
  const grade =
    submission.data ??
    (attempt
      ? { attempt: attempt.id, passed: attempt.passed, results: attempt.results }
      : undefined)
  const readonly = Boolean(grade) || submission.isPending
  const answers = buildAnswers(quiz.questions, draft)
  const update = (id: string, value: number[]) => setDraft((draft) => ({ ...draft, [id]: value }))
  return (
    <form
      className="classroom-quiz"
      onSubmit={(event) => {
        event.preventDefault()
        if (answers && !readonly) submission.mutate()
      }}
    >
      {quiz.questions.map((question, number) => {
        const values = draft[question.id] ?? []
        const result = grade?.results.find((result) => result.question_id === question.id)
        return (
          <section className="classroom-card" key={question.id}>
            <fieldset disabled={readonly}>
              <legend>
                {number + 1}. {question.prompt}
              </legend>
              <p className="text-muted-foreground">
                {t(
                  question.kind === 'single'
                    ? 'classSingleHint'
                    : question.kind === 'multiple'
                      ? 'classMultipleHint'
                      : 'classMatchHint',
                )}
              </p>
              {question.kind === 'match'
                ? question.left.map((left, index) => (
                    <label className="classroom-match" key={index}>
                      <span>{left}</span>
                      <select
                        aria-label={t('classPair', { name: left })}
                        value={values[index] ?? ''}
                        onChange={(event) => {
                          const pairs = Array.from(
                            { length: question.left.length },
                            (_, i) => values[i] ?? -1,
                          )
                          pairs[index] = event.target.value === '' ? -1 : Number(event.target.value)
                          update(question.id, pairs)
                        }}
                      >
                        <option value="">{t('classChoosePair')}</option>
                        {question.right.map((right, i) => (
                          <option key={i} value={i}>
                            {right}
                          </option>
                        ))}
                      </select>
                    </label>
                  ))
                : question.options.map((option, index) => (
                    <label className="classroom-choice" key={index}>
                      <input
                        type={question.kind === 'single' ? 'radio' : 'checkbox'}
                        name={question.id}
                        value={index}
                        checked={values.includes(index)}
                        onChange={() =>
                          update(
                            question.id,
                            question.kind === 'single' ? [index] : toggleOption(values, index),
                          )
                        }
                      />
                      <span>{option}</span>
                    </label>
                  ))}
            </fieldset>
            {result && (
              <div role="status">
                <strong>{t(result.correct ? 'classCorrect' : 'classIncorrect')}</strong>
                {result.explanation && <Markdown>{result.explanation}</Markdown>}
              </div>
            )}
          </section>
        )
      })}
      {!grade && (
        <>
          <p>{t('classAllAnswers')}</p>
          <Button type="submit" variant="primary" disabled={!answers || submission.isPending}>
            {t('classSubmit')}
          </Button>
        </>
      )}
      {submission.isError && <p role="alert">{t('classError')}</p>}
      {grade && (
        <section className="classroom-card" aria-live="polite">
          <h3>{t(grade.passed ? 'classPassed' : 'classNotPassed')}</h3>
          {attempt?.feedback ? (
            <Markdown>{attempt.feedback}</Markdown>
          ) : (
            <p role="status">{t('classFeedbackWaiting')}</p>
          )}
          <Button onClick={onRetry}>{t('classNewQuiz')}</Button>
        </section>
      )}
    </form>
  )
}

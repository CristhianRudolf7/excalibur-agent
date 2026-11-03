import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { CheckCircle2, LockKeyhole } from 'lucide-react'
import { Button } from '@/components/ui/button'
import type { Course } from '@/lib/rpc/classroom'
import { Progress } from './Progress'
import { examUnlocked } from './logic'
import { SyllabusEditor } from './SyllabusEditor'
export function CoursePage({
  course,
  onTopic,
  onExam,
}: {
  course: Course
  onTopic: (id: string) => void
  onExam: () => void
}) {
  const { t } = useTranslation()
  const [editing, setEditing] = useState(false)
  if (editing) return <SyllabusEditor course={course} onClose={() => setEditing(false)} />
  const unlocked = examUnlocked(course)
  return (
    <>
      <div className="classroom-actions">
        <h2>{course.title}</h2>
        <Button onClick={() => setEditing(true)}>{t('classEdit')}</Button>
      </div>
      <p>{course.goal}</p>
      <p>{course.level}</p>
      <Progress value={course.progress} passed={course.final_exam.passed} />
      {course.units.map((unit) => (
        <section className="classroom-unit" key={unit.id}>
          {unit.title && <h3>{unit.title}</h3>}
          <ol className="classroom-topics">
            {unit.topics.map((topic) => (
              <li key={topic.id}>
                <button className="classroom-topic" onClick={() => onTopic(topic.id)}>
                  <span>
                    <strong>{topic.title}</strong>
                    <small>{topic.summary}</small>
                  </span>
                  <span className={`classroom-status status-${topic.status}`}>
                    {t(`classStatus_${topic.status}`)}
                  </span>
                </button>
              </li>
            ))}
          </ol>
        </section>
      ))}
      <section className="classroom-card">
        <h3>{t('classFinal')}</h3>
        <p>
          {t(
            course.final_exam.passed
              ? 'classPassed'
              : unlocked
                ? 'classFinalReady'
                : 'classFinalLocked',
          )}
        </p>
        {course.final_exam.passed ? (
          <CheckCircle2 aria-label={t('classPassed')} />
        ) : (
          <Button disabled={!unlocked} onClick={onExam}>
            {!unlocked && <LockKeyhole size={16} aria-hidden="true" />}
            {t('classStartExam')}
          </Button>
        )}
      </section>
    </>
  )
}

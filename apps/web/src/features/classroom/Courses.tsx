import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { BookOpen, Trash2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { rpc } from '@/lib/rpc/client'
import type { CourseSummary } from '@/lib/rpc/classroom'
import { classroomKey } from './api'
import { Progress } from './Progress'
export function Courses({
  courses,
  onOpen,
}: {
  courses: CourseSummary[]
  onOpen: (id: string) => void
}) {
  const { t, i18n } = useTranslation()
  const client = useQueryClient()
  const [removing, setRemoving] = useState<string>()
  const deletion = useMutation({
    mutationFn: (id: string) => rpc.call('classroom.delete', { course_id: id }),
    onSuccess: () => {
      setRemoving(undefined)
      void client.invalidateQueries({ queryKey: classroomKey })
    },
  })
  return (
    <>
      <div className="classroom-cards">
        {courses.map((course) => (
          <article key={course.id} className="classroom-card">
            <BookOpen aria-hidden="true" />
            <h2>{course.title}</h2>
            <Progress value={course.progress} passed={course.progress === 100} />
            <time dateTime={course.updated_at}>
              {t('classLastActivity', {
                date: new Date(course.updated_at).toLocaleString(i18n.language),
              })}
            </time>
            <div className="classroom-actions">
              <Button onClick={() => onOpen(course.id)}>{t('classOpen')}</Button>
              <Button
                aria-label={t('classDelete')}
                onClick={() => {
                  deletion.reset()
                  setRemoving(course.id)
                }}
              >
                <Trash2 size={16} />
              </Button>
            </div>
          </article>
        ))}
      </div>
      {removing && (
        <Dialog
          title="classDelete"
          onClose={() => {
            if (!deletion.isPending) setRemoving(undefined)
          }}
        >
          <p>{t('classDeleteConfirm')}</p>
          {deletion.isError && <p role="alert">{t('classError')}</p>}
          <Button
            variant="destructive"
            disabled={deletion.isPending}
            onClick={() => deletion.mutate(removing)}
          >
            {t('classDelete')}
          </Button>
        </Dialog>
      )}
    </>
  )
}

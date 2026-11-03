import { useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Dialog } from '@/components/ui/dialog'
import { rpc } from '@/lib/rpc/client'
import type { Course, Unit, SyllabusUnit } from '@/lib/rpc/classroom'
import { reorder } from './logic'
import { classroomKey } from './api'
export function SyllabusEditor({ course, onClose }: { course: Course; onClose: () => void }) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [units, setUnits] = useState<Unit[]>(() => structuredClone(course.units))
  const [removing, setRemoving] = useState<{ unit: string; topic?: string }>()
  const save = useMutation({
    mutationFn: () => {
      const existingUnits = new Set(course.units.map((unit) => unit.id))
      const existingTopics = new Set(
        course.units.flatMap((unit) => unit.topics.map((topic) => topic.id)),
      )
      const syllabus: SyllabusUnit[] = units.map((unit) => ({
        ...(existingUnits.has(unit.id) ? { id: unit.id } : {}),
        title: unit.title,
        topics: unit.topics.map((topic) => ({
          ...(existingTopics.has(topic.id) ? { id: topic.id } : {}),
          title: topic.title,
          summary: topic.summary,
        })),
      }))
      return rpc.call('classroom.update_syllabus', { course_id: course.id, units: syllabus })
    },
    onSuccess: () => {
      void client.invalidateQueries({ queryKey: classroomKey })
      onClose()
    },
  })
  const update = (id: string, fn: (unit: Unit) => Unit) =>
    setUnits((units) => units.map((unit) => (unit.id === id ? fn(unit) : unit)))
  const remove = (unitId: string, topicId?: string) =>
    setUnits((units) =>
      topicId
        ? units.map((unit) =>
            unit.id === unitId
              ? { ...unit, topics: unit.topics.filter((topic) => topic.id !== topicId) }
              : unit,
          )
        : units.filter((unit) => unit.id !== unitId),
    )
  const requestRemove = (unit: Unit, topicId?: string) => {
    const topics = topicId ? unit.topics.filter((topic) => topic.id === topicId) : unit.topics
    if (topics.some((topic) => topic.status === 'completed'))
      setRemoving({ unit: unit.id, topic: topicId })
    else remove(unit.id, topicId)
  }
  const orderButtons = (
    index: number,
    length: number,
    move: (direction: number) => void,
    name: string,
  ) => (
    <>
      <Button
        aria-label={t('classMoveUp', { name })}
        disabled={index === 0}
        onClick={() => move(-1)}
      >
        ↑
      </Button>
      <Button
        aria-label={t('classMoveDown', { name })}
        disabled={index === length - 1}
        onClick={() => move(1)}
      >
        ↓
      </Button>
    </>
  )
  return (
    <form
      className="classroom-editor"
      onSubmit={(event) => {
        event.preventDefault()
        save.mutate()
      }}
    >
      <h2>{t('classEdit')}</h2>
      <fieldset disabled={save.isPending}>
        {units.map((unit, index) => (
          <section className="classroom-card" key={unit.id}>
            <div className="classroom-actions">
              <label>
                {t('classUnitTitle')}
                <input
                  value={unit.title}
                  onChange={(event) =>
                    update(unit.id, (u) => ({ ...u, title: event.target.value }))
                  }
                />
              </label>
              {orderButtons(
                index,
                units.length,
                (direction) => setUnits((units) => reorder(units, index, direction)),
                unit.title || t('classUnitTitle'),
              )}
              <Button onClick={() => requestRemove(unit)}>{t('classRemoveUnit')}</Button>
            </div>
            {unit.topics.map((topic, topicIndex) => (
              <div className="classroom-editor-topic" key={topic.id}>
                <label>
                  {t('classTopicTitle')}
                  <input
                    required
                    value={topic.title}
                    onChange={(event) =>
                      update(unit.id, (u) => ({
                        ...u,
                        topics: u.topics.map((item) =>
                          item.id === topic.id ? { ...item, title: event.target.value } : item,
                        ),
                      }))
                    }
                  />
                </label>
                <label>
                  {t('classTopicSummary')}
                  <input
                    value={topic.summary}
                    onChange={(event) =>
                      update(unit.id, (u) => ({
                        ...u,
                        topics: u.topics.map((item) =>
                          item.id === topic.id ? { ...item, summary: event.target.value } : item,
                        ),
                      }))
                    }
                  />
                </label>
                <div className="classroom-actions">
                  {orderButtons(
                    topicIndex,
                    unit.topics.length,
                    (direction) =>
                      update(unit.id, (u) => ({
                        ...u,
                        topics: reorder(u.topics, topicIndex, direction),
                      })),
                    topic.title,
                  )}
                  <label>
                    {t('classMoveUnit')}
                    <select
                      value={unit.id}
                      onChange={(event) => {
                        const destination = event.target.value
                        setUnits((units) =>
                          units.map((u) =>
                            u.id === destination
                              ? { ...u, topics: [...u.topics, topic] }
                              : u.id === unit.id
                                ? { ...u, topics: u.topics.filter((item) => item.id !== topic.id) }
                                : u,
                          ),
                        )
                      }}
                    >
                      {units.map((u, i) => (
                        <option key={u.id} value={u.id}>
                          {u.title || t('classUnitNumber', { number: i + 1 })}
                        </option>
                      ))}
                    </select>
                  </label>
                  <Button onClick={() => requestRemove(unit, topic.id)}>
                    {t('classRemoveTopic')}
                  </Button>
                </div>
              </div>
            ))}
            <Button
              onClick={() =>
                update(unit.id, (u) => ({
                  ...u,
                  topics: [
                    ...u.topics,
                    {
                      id: crypto.randomUUID(),
                      title: t('classNewTopic'),
                      summary: '',
                      status: 'not_started',
                    },
                  ],
                }))
              }
            >
              {t('classAddTopic')}
            </Button>
          </section>
        ))}
        <Button
          onClick={() =>
            setUnits((units) => [
              ...units,
              { id: crypto.randomUUID(), title: t('classNewUnit'), topics: [] },
            ])
          }
        >
          {t('classAddUnit')}
        </Button>
        <div className="classroom-actions">
          <Button type="submit" variant="primary">
            {t('classSave')}
          </Button>
          <Button onClick={onClose}>{t('classCancel')}</Button>
        </div>
      </fieldset>
      {save.isError && <p role="alert">{t('classError')}</p>}
      {removing && (
        <Dialog title="classRemoveCompleted" onClose={() => setRemoving(undefined)}>
          <p>{t('classRemoveCompletedHint')}</p>
          <Button
            variant="destructive"
            onClick={() => {
              remove(removing.unit, removing.topic)
              setRemoving(undefined)
            }}
          >
            {t('classConfirmRemove')}
          </Button>
        </Dialog>
      )}
    </form>
  )
}

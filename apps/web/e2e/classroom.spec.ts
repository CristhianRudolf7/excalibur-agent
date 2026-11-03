import { test, expect } from './fixtures'
import { mockEvents } from './event-fixture'
import { courseFixture } from '../src/features/classroom/fixtures'
import type { Attempt, Course, QuizView } from '../src/lib/rpc/classroom'

for (const mobile of [false, true]) {
  test(`classroom course, lessons, quizzes and final exam ${mobile ? 'on mobile' : 'on desktop'}`, async ({
    page,
  }) => {
    test.setTimeout(120_000)
    await page.setViewportSize(mobile ? { width: 390, height: 844 } : { width: 1440, height: 1000 })
    const events = await mockEvents(page)
    const course: Course = structuredClone(courseFixture)
    let created = false
    let pending: QuizView | undefined
    let sequence = 0
    const quizzes = new Map<string, QuizView>()
    const attempts: Attempt[] = []
    const contexts: unknown[] = []
    await page.route('**/rpc', async (route) => {
      const { id, method, params } = route.request().postDataJSON()
      let result: unknown = {}
      if (method === 'conversation.send') {
        contexts.push(params)
        return route.fallback()
      }
      if (method === 'classroom.list')
        result = {
          courses: created
            ? [
                {
                  ...course,
                  total_topics: 2,
                  completed_topics: course.units[0]!.topics.filter(
                    (topic) => topic.status === 'completed',
                  ).length,
                },
              ]
            : [],
        }
      else if (method === 'classroom.get') result = course
      else if (method === 'classroom.open_topic') {
        const topic = course.units[0]!.topics.find((topic) => topic.id === params.topic_id)!
        if (topic.status === 'not_started') topic.status = 'in_progress'
        if (!topic.class)
          events.emit('classroom.class_requested', { course_id: course.id, topic_id: topic.id })
        result = { topic, class: topic.class }
      } else if (method === 'whiteboard.get')
        result = {
          id: params.board_id,
          conversation_id: 'cnv_web_excalibur',
          title: 'Equations board',
          version: 0,
          updated_at: course.updated_at,
          elements: [
            {
              id: 'text',
              type: 'text',
              x: 20,
              y: 20,
              text: 'Both sides stay balanced',
              author: 'agent',
            },
          ],
        }
      else if (method === 'classroom.request_quiz') {
        const quizId = `quiz-${++sequence}`
        pending = {
          id: quizId,
          course_id: course.id,
          topic_id: params.topic_id,
          final: params.final ?? false,
          created_at: course.updated_at,
          questions: params.final
            ? Array.from({ length: 10 }, (_, i) => ({
                id: `final-${i}`,
                kind: 'single' as const,
                prompt: `Final question ${i + 1}`,
                options: ['First', 'Second'],
              }))
            : [
                {
                  id: `${quizId}-single`,
                  kind: 'single',
                  prompt: `Variable question ${sequence}`,
                  options: ['x', '3'],
                },
                {
                  id: `${quizId}-multiple`,
                  kind: 'multiple',
                  prompt: 'Select expressions',
                  options: ['x + 1', 'y - 2', '='],
                },
                {
                  id: `${quizId}-match`,
                  kind: 'match',
                  prompt: 'Pair terms',
                  left: ['Variable', 'Constant'],
                  right: ['5', 'y'],
                },
              ],
        }
        quizzes.set(quizId, pending)
        events.emit('classroom.quiz_requested', {
          course_id: course.id,
          topic_id: params.topic_id,
          final: params.final,
        })
        result = { requested: true }
      } else if (method === 'classroom.get_quiz') result = quizzes.get(params.quiz_id)
      else if (method === 'classroom.submit') {
        const quiz = quizzes.get(params.quiz_id)!
        const passed = params.quiz_id !== 'quiz-1'
        const attempt: Attempt = {
          id: `attempt-${attempts.length + 1}`,
          quiz_id: quiz.id,
          passed,
          results: quiz.questions.map((question, i) => ({
            question_id: question.id,
            correct: passed || i !== 1,
            explanation: 'Review the lesson and the reasoning.',
          })),
          created_at: course.updated_at,
        }
        attempts.push(attempt)
        if (passed) {
          if (quiz.final) {
            course.final_exam.passed = true
            course.progress = 100
          } else {
            course.units[0]!.topics.find((topic) => topic.id === quiz.topic_id)!.status =
              'completed'
            const completed = course.units[0]!.topics.filter(
              (topic) => topic.status === 'completed',
            ).length
            course.progress = Math.floor((completed / 2) * 99)
            course.final_exam.unlocked = completed === 2
          }
          events.emit(quiz.final ? 'classroom.course_completed' : 'classroom.topic_completed', {
            course_id: course.id,
            topic_id: quiz.topic_id,
          })
        }
        result = { attempt: attempt.id, passed, results: attempt.results }
      } else if (method === 'classroom.attempts')
        result = {
          attempts: attempts.filter(
            (attempt) => quizzes.get(attempt.quiz_id)?.topic_id === params.topic_id,
          ),
        }
      else return route.fallback()
      await route.fulfill({ json: { jsonrpc: '2.0', id, result } })
    })
    await page.goto('/classroom')
    await events.ready('classroom.course_created')
    await expect(page.getByRole('heading', { name: 'Aula', exact: true })).toBeVisible()
    await expect(page.getByRole('heading', { name: '¿Qué quieres estudiar?' })).toBeVisible()
    const composer = page.getByRole('textbox', { name: 'Escribe a Excalibur…' })
    await composer.fill('I want to study algebra')
    await composer.press('Enter')
    await expect.poll(() => contexts.length).toBe(1)
    expect(contexts[0]).toMatchObject({
      conversation_id: 'cnv_web_excalibur',
      context: { app: 'classroom' },
    })
    created = true
    events.emit('classroom.course_created', { course_id: course.id })
    await expect(page.getByRole('heading', { name: course.title })).toBeVisible()
    await expect(page.getByRole('button', { name: 'Comenzar examen' })).toBeDisabled()
    // Open a later topic first, without completing the earlier one.
    await page.getByRole('button', { name: /Equations/ }).click()
    await expect(page.getByText('Excalibur está preparando la clase…')).toBeVisible()
    course.units[0]!.topics[1]!.class = {
      format: 'whiteboard',
      content: '**Balance** both sides of an equation.',
      whiteboard_id: 'lesson-board',
      created_at: course.updated_at,
    }
    events.emit('classroom.class_ready', { course_id: course.id, topic_id: 'topic-2' })
    await expect(page.locator('.classroom-lesson strong')).toHaveText('Balance')
    await expect(page.locator('.classroom-whiteboard')).toBeVisible()
    await expect(page.getByText('Equations board', { exact: true })).toBeVisible()
    await expect(page).toHaveURL(/\/classroom$/)
    await page.getByRole('button', { name: 'Conversación del aula', exact: true }).click()
    const chat = page.getByRole(mobile ? 'dialog' : 'region', { name: 'Conversación del aula' })
    await chat.getByRole('textbox').fill('Explain this topic again')
    await chat.getByRole('textbox').press('Enter')
    await expect.poll(() => contexts.length).toBe(2)
    expect(contexts[1]).toMatchObject({
      conversation_id: 'cnv_web_excalibur',
      context: { app: 'classroom', course_id: course.id, topic_id: 'topic-2' },
    })
    await page.keyboard.press('Escape')
    await expect(chat).toHaveCount(0)

    const deliverQuiz = async (expected: number) => {
      await expect.poll(() => pending?.id).toBe(`quiz-${expected}`)
      await expect(page.getByText('Excalibur está preparando preguntas nuevas…')).toBeVisible()
      const quiz = pending!
      events.emit('classroom.quiz_ready', {
        course_id: course.id,
        topic_id: quiz.topic_id,
        final: quiz.final,
        quiz_id: quiz.id,
      })
      await expect(page.getByRole('radio').first()).toBeVisible()
    }
    const answerTopicQuiz = async () => {
      await page.getByRole('radio', { name: 'x', exact: true }).check()
      await page.getByRole('checkbox', { name: 'x + 1', exact: true }).check()
      await page.getByRole('checkbox', { name: 'y - 2', exact: true }).check()
      await page.getByRole('combobox', { name: 'Pareja de Variable' }).selectOption('1')
      await page.getByRole('combobox', { name: 'Pareja de Constant' }).selectOption('0')
      await page.getByRole('button', { name: 'Enviar respuestas' }).click()
    }
    await page.getByRole('button', { name: 'Comenzar cuestionario' }).click()
    await deliverQuiz(1)
    await answerTopicQuiz()
    await expect(page.getByText('Incorrecto', { exact: true })).toBeVisible()
    attempts[0]!.feedback = 'Practice identifying expressions before retrying.'
    events.emit('classroom.feedback_ready', {
      course_id: course.id,
      topic_id: 'topic-2',
      attempt_id: attempts[0]!.id,
    })
    await expect(page.getByText(attempts[0]!.feedback, { exact: true }).first()).toBeVisible()
    await page.getByRole('button', { name: 'Intentar con preguntas nuevas' }).click()
    await deliverQuiz(2)
    await expect(page.getByText('1. Variable question 2')).toBeVisible()
    await answerTopicQuiz()
    await expect(page.getByRole('heading', { name: 'Aprobado' })).toBeVisible()
    await page.getByRole('button', { name: 'Volver al temario' }).click()
    // Leaving the quiz returns to its topic; the topic then returns to the syllabus.
    await page.getByRole('button', { name: 'Volver al temario' }).click()
    await expect(page.getByRole('progressbar')).toHaveAttribute('value', '49')
    course.units[0]!.topics[0]!.class = {
      format: 'chat',
      content: 'Variables name values.',
      created_at: course.updated_at,
    }
    await page.getByRole('button', { name: /Variables/ }).click()
    await page.getByRole('button', { name: 'Comenzar cuestionario' }).click()
    await deliverQuiz(3)
    await answerTopicQuiz()
    await expect(page.getByRole('heading', { name: 'Aprobado' })).toBeVisible()
    await page.getByRole('button', { name: 'Volver al temario' }).click()
    await page.getByRole('button', { name: 'Volver al temario' }).click()
    await expect(page.getByRole('progressbar')).toHaveAttribute('value', '99')
    await page.getByRole('button', { name: 'Comenzar examen' }).click()
    await deliverQuiz(4)
    for (const radio of await page.getByRole('radio', { name: 'First', exact: true }).all())
      await radio.check()
    await page.getByRole('button', { name: 'Enviar respuestas' }).click()
    await expect(page.getByRole('heading', { name: 'Aprobado' })).toBeVisible()
    await page.getByRole('button', { name: 'Volver al temario' }).click()
    await expect(page.getByRole('progressbar')).toHaveAttribute('value', '100')
    expect(
      await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth),
    ).toBe(true)
    if (mobile)
      await page
        .locator('.chat-header')
        .getByRole('button', { name: 'Alternar barra lateral', exact: true })
        .click()
    await page.getByRole('button', { name: 'Proyectos', exact: true }).click()
    await expect(page.getByRole('heading', { name: 'Proyectos', exact: true })).toBeVisible()
    await expect(page).toHaveURL(/\/projects$/)
  })
}

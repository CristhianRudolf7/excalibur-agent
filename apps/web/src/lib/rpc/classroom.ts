/** Client views from docs/development/classroom.md. Answer keys never belong here. */
export interface ClassroomContext {
  app: 'classroom'
  course_id?: string
  topic_id?: string
}
export interface Lesson {
  format: 'chat' | 'whiteboard'
  content: string
  whiteboard_id?: string
  created_at: string
}
export interface Topic {
  id: string
  title: string
  summary: string
  status: 'not_started' | 'in_progress' | 'completed'
  class?: Lesson
  completed_at?: string
}
export interface Unit {
  id: string
  title: string
  topics: Topic[]
}
export interface SyllabusUnit {
  id?: string
  title: string
  topics: { id?: string; title: string; summary: string }[]
}
export interface Course {
  id: string
  title: string
  goal: string
  level: string
  language: string
  units: Unit[]
  final_exam: { unlocked: boolean; passed: boolean; passed_at?: string }
  progress: number
  created_at: string
  updated_at: string
  completed_at?: string
}
export interface CourseSummary {
  id: string
  title: string
  progress: number
  updated_at: string
  total_topics: number
  completed_topics: number
}
export type QuestionView =
  | { id: string; kind: 'single' | 'multiple'; prompt: string; options: string[] }
  | { id: string; kind: 'match'; prompt: string; left: string[]; right: string[] }
export interface QuizView {
  id: string
  course_id: string
  topic_id?: string
  final: boolean
  questions: QuestionView[]
  created_at: string
}
export interface Answer {
  question_id: string
  selected?: number[]
  pairs?: number[]
}
export interface AnswerResult {
  question_id: string
  correct: boolean
  explanation?: string
}
export interface Attempt {
  id: string
  quiz_id: string
  results: AnswerResult[]
  passed: boolean
  feedback?: string
  created_at: string
}
export interface Grade {
  attempt: string
  passed: boolean
  results: AnswerResult[]
}
export interface ClassroomEventPayload {
  course_id?: string
  topic_id?: string
  quiz_id?: string
  attempt_id?: string
  final?: boolean
  feedback?: string
  course?: Course
  deleted?: boolean
  class?: Lesson
  quiz?: QuizView
  passed?: boolean
  results?: AnswerResult[]
  topic?: Topic
  progress?: number
}
export const classroomEventTypes = [
  'classroom.course_created',
  'classroom.course_updated',
  'classroom.class_requested',
  'classroom.class_ready',
  'classroom.quiz_requested',
  'classroom.quiz_ready',
  'classroom.attempt_graded',
  'classroom.feedback_ready',
  'classroom.topic_completed',
  'classroom.course_completed',
] as const
export interface ClassroomMethods {
  'classroom.list': { params: undefined; result: { courses: CourseSummary[] } }
  'classroom.get': { params: { course_id: string }; result: Course }
  'classroom.update_syllabus': {
    params: { course_id: string; units: SyllabusUnit[] }
    result: Course
  }
  'classroom.delete': { params: { course_id: string }; result: { ok: boolean } }
  'classroom.open_topic': {
    params: { course_id: string; topic_id: string }
    result: { topic: Topic; class?: Lesson }
  }
  'classroom.request_quiz': {
    params: { course_id: string; topic_id?: string; final?: boolean }
    result: { requested: boolean; quiz?: QuizView }
  }
  'classroom.get_quiz': { params: { quiz_id: string }; result: QuizView }
  'classroom.submit': { params: { quiz_id: string; answers: Answer[] }; result: Grade }
  'classroom.attempts': {
    params: { course_id: string; topic_id?: string }
    result: { attempts: Attempt[] }
  }
}

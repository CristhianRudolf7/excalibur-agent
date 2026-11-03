# Classroom web interface

The `/classroom` route uses the shared shell, sidebar, header controls and
right panel styles. The feature follows the course rules and conversation
model in `docs/development/classroom.md` and Section 14.8. All visible controls
have English and Spanish translations.

The typed RPC interface lives in `src/lib/rpc/classroom.ts`. It contains client
views of courses, lessons, quizzes, attempts and grading results. Quiz question
views have no answer-key fields or pre-grading explanations. Matching answers
contain only the user's selected right-hand indexes. Grading results provide a
boolean and explanation for each question; grading belongs to the daemon.

## Conversation and live state

Every classroom message uses the existing `excalibur` chat identity and history.
The optional `conversation.send.context` contains `app: "classroom"`, plus the
selected course and topic IDs. Failed-message retries retain their original
context and client message ID. Lesson regeneration also uses this conversation.
The classroom does not create separate course or topic conversations.

TanStack Query owns persisted data. The Zustand store owns selection and pending
lesson/quiz state keyed by course and topic (or final exam). Classroom event
payloads identify their target with `course_id`, `topic_id`, `quiz_id` and
`attempt_id` as applicable. Events invalidate the persisted queries; reconnects
refresh those queries after event replay. Historical course-created events do
not replace the user's selection. A live course-created event opens its course.

`classroom.open_topic` uses a separate query key because it has a side effect.
Refreshing grading or feedback data must not request another class. Quiz entry
calls `classroom.request_quiz`; an existing unanswered quiz can be reused by the
server. After grading, retry requests a new quiz and clears the previous draft.

Whiteboard lessons pin the shared whiteboard feature to their stored board ID.
Opening that board while in the classroom keeps the classroom route active.
The markdown version remains available alongside the board.

## Verification and integration

Vitest covers syllabus operations, completion and exam locks, answer building,
question controls, hidden answer-key fields, events, regeneration, retries,
feedback and mobile chat behavior. `e2e/classroom.spec.ts` uses mocked RPC and
WebSocket events for desktop and mobile flows through course creation, a later
topic first, whiteboard preparation, contextual chat, a failed quiz, fresh
questions, topic completion and the final exam.

The classroom backend and agent are implemented on parallel branches. This
branch validates the web interface against the documented contract and mocks;
it does not claim a live daemon integration check. Merge the backend's API and
exported method/event schemas together with this interface before deployment.

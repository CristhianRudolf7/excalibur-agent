# 0099: Classroom state and client boundaries

The classroom contract implements the application rules of Section 14.8 through
an additive daemon service. Migration 10 stores courses (including their units,
topics and lessons), server quizzes with answer keys, and attempts with feedback
in the existing main database. Each state transition uses one porter command.
Deleting a course removes its quizzes and attempts in the same transaction.

`internal/classroom.Service` exposes `CreateCourse`, `GetCourse`, `ListCourses`,
`DeleteCourse`, `UpdateSyllabus`, `OpenTopic`, `WriteClass`, `CreateQuiz`,
`RequestQuiz`, `GetQuiz`, `Submit`, `WriteFeedback` and `Attempts`. Trusted agent
callers use server models; client routes and events use explicit views from
`internal/api/classroom.go`. Question views omit answer keys and explanations.
Attempt views omit submitted answers and expose only grading results, feedback
and identifiers. Explanations appear after grading.

Topic completion requires a completely correct submission. A quiz accepts one
attempt; retries require another quiz. Syllabus edits retain stored state for
existing topic IDs and assign new IDs to additions. Adding an incomplete topic
revokes final-exam completion. Removing topics retains historical attempts while
recomputing progress and the final-exam size against the current topics.

Production startup injects the same persistent classroom service into daemon
assembly and RPC routing. `Services.Classroom`, `AssembleOptions.Classroom` and
`BackendServices.Classroom` are the additive integration seams for the agent
tool and request-event worker. Assembly supplies an isolated memory service
when no persistent service is injected.

`conversation.send` accepts an optional `context` object with `app: classroom`,
`course_id` and optional `topic_id`. The gateway carries it in request context;
master intake copies it onto that message's normalized envelope before queueing.
The daemon restores it for tool execution and includes it in the turn snapshot.
The existing conversation ID, text and single history remain the conversation
boundary. An empty classroom may omit course and topic IDs.

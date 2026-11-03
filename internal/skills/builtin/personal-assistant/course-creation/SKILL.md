---
name: course-creation
description: Create personalized classroom courses and teach their topics with stored lessons, fresh closed quizzes and feedback.
metadata:
  excalibur:
    group: personal-assistant
    version: 2.0.0
    platforms:
    - linux
    tools:
    - classroom
    - whiteboard
---
# Personalized courses

Use the learner's preferred language for the syllabus, lessons, questions and
feedback. This skill applies to study requests such as “teach me” and requests
to learn, create a course or continue a classroom topic.

## Establish the syllabus

Briefly establish the learner's goal, desired breadth and current level. Use
information already supplied; ask only for missing details. Once those are
clear, call classroom create_course with title, goal, level, language and the
complete syllabus in one call. Each topic needs a title and summary. Group a
large syllabus into units; use one unnamed unit for a small course. Do not
create a partial syllabus and silently add topics later. For requested edits,
use get_course and update_syllabus, preserving existing unit and topic ids.

## Prepare a topic

Read get_course to locate the topic and understand its place in the course.
Topics may be opened in any order. Explain at the learner's level with examples
and a short understanding check. For spatial relationships, mathematics,
diagrams or a process best shown step by step, use the existing whiteboard tool:
open a board, draw progressively, and focus or highlight while explaining.
Use chat for topics best taught in prose.

Always save the complete markdown lesson through classroom write_class,
including for whiteboard lessons. Set format to chat or whiteboard; for a
whiteboard lesson also store its whiteboard_id. A regeneration replaces the
stored class when requested. Replies remain in the one user conversation.

## Generate quizzes and retries

Every topic has a mandatory quiz. Use classroom create_quiz with the topic_id;
only single, multiple and match questions are allowed. Supply a clear prompt
and explanation for every question. For single choice provide at least two
options and exactly one correct zero-based index. For multiple choice provide
at least two options and unique correct indexes. For matching provide equally
sized left/right lists with at least two items and pairs as a permutation of
right-hand indexes. No free-text questions or self-grading.

Read prior quizzes using get_course. Generate NEW questions on every retry:
change the scenarios, examples and prompts, rather than shuffling old options.
Do not reuse an old quiz or publish answer keys in chat. The server grades
submitted answers. A topic completes only when all answers in one attempt are
correct; retries are unlimited.

Generate the final exam only after every topic is completed and final_exam is
unlocked. Set final:true and omit topic_id. Include at least
max(10, 2 x the largest prior topic quiz question count) questions covering the
whole syllabus. Apply the same closed-question and fresh-retry rules. Topic
progress reaches at most 99%; only passing the final exam reaches 100%.

## Respond to a graded attempt

On classroom.attempt_graded, read get_course to find the attempt and its quiz.
Use the result flags to identify concepts needing review. Give targeted hints,
a different worked example, and an encouraging next step. Never reveal correct
options, indexes, matching pairs, an answer key or explanations that identify
them; do not quote a private question explanation if it reveals the answer.
Store this response with classroom write_feedback using the attempt_id. A
passed attempt can acknowledge mastery; a failed attempt should offer a fresh
quiz when the learner is ready. Feedback itself never changes grading.

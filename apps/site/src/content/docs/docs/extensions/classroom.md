---
title: 'Classroom'
description: 'Create a course, study a topic, and review quiz feedback.'
---

Use Classroom to keep a learning goal, syllabus, lessons, and quiz attempts together. You need a running daemon with the Classroom service and a configured model provider to prepare course material. Enabling the navigation entry alone does not supply a model connection.

## Ask for a course

Open Classroom and use its conversation to describe your subject, current level, goal, and preferred language. For example:

> Create a beginner course on reading SQL queries. I want to understand SELECT, filtering, and joins before writing queries myself. Teach in English.

The assistant can create a course with units and topics. Review the syllabus before starting. You can edit titles and summaries, add or remove topics, and reorder the syllabus through the editor. Removing completed material prompts for confirmation.

## Study one topic

Open a topic to load its lesson. If the lesson is being prepared, wait for the preparation state to finish. Lessons can include Markdown explanations and a linked whiteboard. You can return to the course syllabus or ask the selected agent a follow-up question from the Classroom conversation.

If a lesson needs a different explanation, use the regeneration action. It sends a request to the agent; it is not an immediate offline rewrite. Read the new lesson before continuing.

## Check understanding

Start the topic quiz when ready. The available question types include single choice, multiple choice, and matching. Complete the required answers and submit them for grading. The quiz view reports results, while attempt history keeps earlier attempts and any available feedback.

A passed quiz records the result of that attempt. It is not a professional qualification or proof that every generated explanation is correct. Ask about a confusing result and check source material when it matters.

Classroom uses the selected personal agent. Switching agents changes the conversation used for questions. See [Agents and subagents](/docs/building/agents/) for how histories and shared resources work.

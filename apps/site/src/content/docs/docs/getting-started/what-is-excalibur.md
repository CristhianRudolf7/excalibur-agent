---
title: 'What is Excalibur?'
description: 'Choose a first task for your personal assistant and coding harness.'
---

Excalibur is a personal AI assistant that can work with your files, browser, documents, and connected services. You give it a request in conversation. It can answer directly or use tools to carry out the work. For programming, it can inspect a project, edit files, run commands, and delegate work to subagents.

## Start with one concrete task

Tell Excalibur what you want to finish and what information it should use. For example:

> Search the document I attached for the cancellation terms. Summarize them and cite the relevant passages.

This example needs a connected model provider and a document that has finished processing in the library. Follow [Quick start](/docs/getting-started/quick-start/) for a terminal request, or [Documents and citations](/docs/everyday/documents/) for attachments.

## Choose how to work

The web application includes conversations, live voice, a browser activity panel, personal data collections, and document viewers. Some features need additional setup. Voice needs microphone permission; Google tools need an account connection; browser tasks need a supported browser on the machine running Excalibur.

The command-line application also accepts individual requests and background tasks. Conversation commands connect to the running daemon. A non-interactive request can run through `exec` without opening the web application.

Personal agents have their own conversations and personalities while sharing user memory. Subagents are workers for a particular task. Creating a personal agent does not create an isolated account or a separate machine.

Begin with [installation](/docs/getting-started/install/), then [connect a provider](/docs/getting-started/providers/). The [security and permissions guide](/docs/reference/security/) explains credential entry and actions that require confirmation.

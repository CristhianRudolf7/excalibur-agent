---
title: 'Chat and live voice'
description: 'Work in a conversation or speak with the selected agent.'
---

Open a conversation with the agent you want to use. Each personal agent has its own conversation history. Selecting a different agent changes where new messages and voice turns go. You need a running daemon and a configured model provider for replies.

## Make a request

Describe the outcome, the material to use, and any limit on actions. For example:

> Read the attached meeting notes and make a short list of unresolved questions. Do not send anything.

Attach the notes, wait for processing, and send the message. Document attachments can provide source material for the answer. See [Documents and citations](/docs/everyday/documents/) when you need to check a passage.

The terminal offers a conversation entry point too:

```sh
excalibur chat "Help me organize the next steps for this project."
```

This sends a message to the daemon; it is not an interactive terminal chat window.

## Speak instead of typing

Use the conversation's voice controls and allow microphone access when your browser asks. Live calls can transcribe your speech and play spoken replies. The call overlay includes controls for the microphone, speaker, and ending the call. Ending it releases the microphone used by that call.

Voice depends on browser audio support and the installation's speech services. Excalibur has realtime, server, and browser speech paths, with fallback when a service is unavailable. Availability and voice quality can differ between browsers. Browser speech may use the browser vendor's service, so do not assume that every voice path runs locally.

If microphone access is denied, enable it in browser settings or continue with text. If transcription is incorrect, correct the request in the conversation before asking for an external action. Speaking a request does not bypass [confirmation requirements](/docs/reference/security/) for actions such as sending Gmail.

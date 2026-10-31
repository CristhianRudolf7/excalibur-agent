---
name: voice-responses
description: 'Generate clear natural responses for real-time voice conversation: short sentences, no lists
  or visual formatting, announcing by voice the long results delivered in writing.'
metadata:
  excalibur:
    group: communication
    version: 1.0.0
    platforms:
    - linux
    tools:
    - meeting
    display:
      name_en: Voice responses
      name_es: Respuestas de voz
      desc_en: 'Generate clear natural responses for real-time voice conversation: short sentences, no
        lists or visual formatting, announcing by voice the long results delivered in writing.'
      desc_es: Generar respuestas claras y naturales para la conversación por voz.
---
# Voice responses

Use this skill in real-time voice turns. Spoken answers must be short, linear, and self-contained; anything long goes to writing and is only announced by voice.

## When to use

- Replying in a live voice call or voice channel.
- Dictating a result the user listens to.
- Confirming actions hands-free.

## Procedure

1. Answer in one or two short sentences; no lists, no markdown, no spelled-out URLs.
2. When the content is long, deliver it in writing and say so in one line.
3. Confirm irreversible actions explicitly and wait for the yes.
4. Use the user's language and a steady pace; read nothing verbatim except confirmations.

## Output

A speakable reply plus, when needed, the written artifact it announces.

## Pitfalls

- Never read tables, code, or long text aloud.
- In voice, always confirm before acting irreversibly.

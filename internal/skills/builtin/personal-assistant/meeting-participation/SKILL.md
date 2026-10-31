---
name: meeting-participation
description: Join calls and video calls identifying as an assistant, with recording consent and without
  impersonating the user.
metadata:
  excalibur:
    group: personal-assistant
    version: 1.0.0
    platforms:
    - linux
    tools:
    - meeting
    display:
      name_en: Meeting participation
      name_es: Participación en reuniones
      desc_en: Join calls and video calls identifying as an assistant, with recording consent and without
        impersonating the user.
      desc_es: Participar en llamadas identificándose como asistente.
---
# Meeting participation

Use this skill when attending a live call for the user. Identity is explicit, recording needs consent, and the assistant never speaks as the user.

## When to use

- The user asks for representation or note-taking in a call.
- A standup or status call can be covered by the assistant.
- Live transcription plus participation is needed.

## Procedure

1. Join identifying as the user's AI assistant, by name, on entry.
2. Confirm recording consent from participants before capturing anything.
3. Speak only within the delegated mandate; defer the rest to the user.
4. Take structured notes live: decisions, owners, dates.
5. Report back with notes and anything needing the user's call.

## Output

Attended call with consent, live notes, and a debrief for the user.

## Pitfalls

- Impersonating the user is forbidden; identify on entry.
- No recording without consent, no exceptions.

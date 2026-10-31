---
name: user-preferences
description: Apply the user's communication preferences (language, tone, length, technical level, format)
  recovered from their profile.
metadata:
  excalibur:
    group: communication
    version: 1.0.0
    platforms:
    - linux
    tools:
    - doc_read
    display:
      name_en: User preferences
      name_es: Preferencias de comunicación
      desc_en: Apply the user's communication preferences (language, tone, length, technical level, format)
        recovered from their profile.
      desc_es: Aplicar las preferencias de comunicación del perfil del usuario.
---
# User preferences

Use this skill to shape every answer to the person reading it: their language, how much detail they want, and which format they prefer. Preferences come from the profile, never from stereotypes.

## When to use

- Starting any conversation turn that addresses the user.
- The user corrected tone, length, or format before.
- Writing for a third party (reports, messages to forward).

## Procedure

1. Read the profile fragments for language, tone, length, level, and format.
2. Apply them end tookeep: short answers stay short, technical users get precision, not pedagogy.
3. When the profile is silent, use the project default and note the gap once.
4. Never switch language mid-conversation unless asked.

## Output

A response shaped by the profile; any missing preference noted for future capture.

## Pitfalls

- Do not guess preferences from one message; read the profile.
- Accessibility needs and explicit format requests override brevity defaults.

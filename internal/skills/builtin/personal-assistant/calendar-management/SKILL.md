---
name: calendar-management
description: Check availability, propose times and create or move events in the user's timezone.
metadata:
  excalibur:
    group: personal-assistant
    version: 1.0.0
    platforms:
    - linux
    tools:
    - calendar
    display:
      name_en: Calendar management
      name_es: Gestión del calendario
      desc_en: Check availability, propose times and create or move events in the user's timezone.
      desc_es: Consultar disponibilidad y gestionar eventos en la zona horaria.
---
# Calendar management

Use this skill for scheduling. Availability checked first, proposals with options, and every event in the user's timezone with the right attendees and buffers.

## When to use

- Scheduling, moving, or canceling events.
- Answering 'when am I free' questions.
- Negotiating times across timezones.

## Procedure

1. Check real availability including buffers and working hours before proposing.
2. Propose 2-3 concrete options with timezone stated explicitly.
3. Create or move events only with confirmation for external attendees.
4. Include location, link, agenda, and attendees on every created event.
5. Confirm the final state back: what, when, who.

## Output

Scheduled events with confirmed times, attendees, and timezone.

## Pitfalls

- The timezone is part of the answer; never omit it.
- External invites need confirmation before sending.

---
name: youtube-search
description: Search YouTube videos and get their transcripts when available.
metadata:
  excalibur:
    group: browser
    version: 1.0.0
    platforms:
    - linux
    tools:
    - source_search
    - audio_transcribe
    display:
      name_en: YouTube search
      name_es: Búsqueda en YouTube
      desc_en: Search YouTube videos and get their transcripts when available.
      desc_es: Buscar vídeos y obtener transcripciones.
---
# YouTube search

Use this skill for video knowledge: talks, demos, and walkthroughs whose transcripts make them searchable and skimmable without watching at 1x.

## When to use

- A talk, demo, or tutorial covers the topic.
- Visual demonstration matters more than text.
- Checking what a video actually claims.

## Procedure

1. Search with specific terms; prefer official channels and known practitioners.
2. Get the transcript when available and search within it for the relevant part.
3. Watch the key segment to confirm visual or demo claims.
4. Note the video date: interfaces and APIs shown may be obsolete.
5. Cite with link and timestamp.

## Output

Video findings with transcript excerpts, timestamps, and dates.

## Pitfalls

- Transcripts err on jargon; verify key terms.
- Demos age fast; check the recording date.

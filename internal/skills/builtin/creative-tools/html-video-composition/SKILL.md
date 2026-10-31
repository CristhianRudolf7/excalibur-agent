---
name: html-video-composition
description: Generate video from HTML, CSS and animations rendered frame by frame (later phase).
metadata:
  excalibur:
    group: creative-tools
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - browser_snapshot
    display:
      name_en: HTML video composition
      name_es: Composición de vídeo HTML
      desc_en: Generate video from HTML, CSS and animations rendered frame by frame (later phase).
      desc_es: Generar vídeo desde composiciones HTML renderizadas.
---
# HTML video composition

Use this skill for motion graphics built as web pages. HTML/CSS animation rendered frame by frame gives precise, version-controlled video from code.

## When to use

- Animated titles, overlays, or explainers are needed.
- Design lives in HTML/CSS already.
- Frame-exact reproducible motion is required.

## Procedure

1. Build the composition as a page with timed animations.
2. Render frame by frame at the delivery framerate and resolution.
3. Assemble and check motion smoothness and timing against the spec.
4. Version the composition code alongside the rendered output.
5. Note the later-phase limits of the current renderer.

## Output

Frame-rendered video from a versioned web composition.

## Pitfalls

- Browser-preview timing differs from frame rendering; verify frames.
- Unversioned compositions cannot be reproduced.

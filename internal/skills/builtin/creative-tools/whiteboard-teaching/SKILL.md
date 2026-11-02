---
name: whiteboard-teaching
description: Teach visually using an interactive whiteboard, progressive diagrams, highlights and the user's drawings.
metadata:
  excalibur:
    group: creative-tools
    version: 1.0.0
    platforms:
    - linux
    tools:
    - whiteboard
---
# Teach with the shared whiteboard

Use this skill when a spatial explanation helps: architecture, geometry,
mathematics, a process, or a worked example in a text or voice conversation.

1. Discover the deferred `whiteboard` tool with `search_tools`. Open a board
   with a descriptive title and panel layout; retain the returned board ID.
2. Structure the explanation with a heading, generous spacing and stable
   element IDs. Use native shapes and bound arrows for relationships, `math`
   for LaTeX, `code` for source, and sticky notes for questions.
3. Add a few elements at a time using `animate: true, step_ms: 300`. Narrate
   the relationship being revealed. Use `step` with bounded delays to reveal
   a sequence, and `focus` to keep the relevant area visible.
4. `highlight` IDs using a pulse or laser while explaining a critical point;
   `annotate` a target when the explanation needs a lasting callout.
5. Invite the user to draw their understanding. `read` returns user strokes;
   use `include_image: true` to request a PNG for visual inspection. If the
   snapshot times out, say so and use the element data; never invent a view.
6. Preserve user work: read first, update only the intended fields, and use
   `clear` with `keep_user: true` when resetting your explanation.
7. Check understanding, leave a useful final diagram, offer export when
   helpful, and close the board when the user has finished with it.

Example calls:

```json
{"action":"open","title":"Request lifecycle","layout":"panel"}
{"action":"add","animate":true,"step_ms":300,"elements":[{"id":"request","type":"sticky","x":40,"y":60,"width":180,"height":90,"text":"A request arrives","author":"agent"}]}
{"action":"highlight","ids":["request"],"style":"pulse","duration_ms":1500}
{"action":"read","include_image":true}
```

Keep results compact. Each call supports at most 500 elements and 2 MiB.
A stale user edit is rebased by fetching the current version and resubmitting
only the intended element changes; never overwrite the entire board casually.

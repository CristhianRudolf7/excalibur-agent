---
name: agent-avatars
description: Turn a person's description or character idea into a personal agent's pixel-art AvatarSpec, or revise an existing avatar's appearance.
metadata:
  excalibur:
    group: creative-tools
    version: 1.0.0
    platforms:
    - linux
    tools:
    - design_avatar
---
# Design a personal agent avatar

Translate the user's description into a complete `spec` for `design_avatar`.
Supply the original request in `description`. The tool validates the fields
and returns `{spec, preview}`; use the returned spec, including any contrast
corrections. A description-only call generates a deterministic starting palette;
it does not interpret the character. Designing does not save or change an agent.

Keep the 2D world's chunky pixel-art silhouettes, dark outlines and simple
faces. The same spec renders as a 14 × 14 portrait and a 16 × 28 full sprite.
Choose one silhouette and one overlay; do not invent fields or generate a photo.

| Field | Meaning and allowed values |
| --- | --- |
| `variant` | `hood`, `long-hair`, `cap`, `bow`, `beard`, or `headphones` |
| `tone` | Skin color, including a metal-like skin palette for a robot |
| `hair` | Hair and beard color |
| `highlight` | Light clothing and accessory detail |
| `outfit` | Main clothing, hood or cap color |
| `accent` | Accessory and small decoration color |
| `accessory` | `none`, `glasses`, `sunglasses`, `earring`, `scarf`, or `star` |
| `background` | Quiet backdrop color contrasting with the silhouette |

Every color must be exactly `#RRGGBB`. Use a coordinated palette with readable
dark eyes and distinct hair and skin. Keep the requested colors where possible.
For unspecified details, choose a pleasant complementary palette rather than
asking about every field. Explain any limits when the request needs several
overlays or a silhouette outside the six supported shapes.

A girl with glasses and purple hair maps to `long-hair`, purple `hair`, and
`glasses`. For example:

```json
{"description":"A girl with glasses and purple hair","spec":{"variant":"long-hair","tone":"#F2C9A1","hair":"#8754B3","highlight":"#E5D8FF","outfit":"#586FA8","accent":"#F4C76B","accessory":"glasses","background":"#F0E9FA"}}
```

An orange robot wearing headphones maps to `headphones`, a light metal `tone`,
an orange `outfit`, and orange `accent`. The robot is a stylized character using
the existing face, not a new robot silhouette:

```json
{"description":"An orange robot with headphones","spec":{"variant":"headphones","tone":"#D1DCE5","hair":"#566879","highlight":"#FFE0B2","outfit":"#D67B38","accent":"#E99245","accessory":"none","background":"#E8F0F6"}}
```

Describe the returned appearance briefly in the conversation's language. Offer
to regenerate with a different palette, silhouette or accessory. For revisions,
preserve the previous fields except those the user wants changed, and validate
again. If saving is requested, pass the validated spec to the agent-management
operation available in the session; do not claim persistence from this tool.

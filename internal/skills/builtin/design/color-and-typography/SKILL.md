---
name: color-and-typography
description: Choose color palettes and type combinations for the product, with enough contrast.
metadata:
  excalibur:
    group: design
    version: 1.0.0
    platforms:
    - linux
    tools: []
    display:
      name_en: Color and typography
      name_es: Color y tipografía
      desc_en: Choose color palettes and type combinations for the product, with enough contrast.
      desc_es: Elegir paletas y tipografías con contraste suficiente.
---
# Color and typography

Use this skill for palette and type decisions. Contrast-checked combinations that survive both light and dark contexts keep the product readable for everyone.

## When to use

- Defining or changing brand colors or fonts.
- Text legibility complaints appear.
- Designing for varied lighting and vision abilities.

## Procedure

1. Pick a palette with roles: background, surface, primary, accent, danger, text.
2. Pair at most two typefaces with clear jobs: display versus body.
3. Check contrast ratios for every text-on-background pair against WCAG AA.
4. Verify the palette under simulated color-vision deficiencies.
5. Record the tokens so code and design share the same values.

## Output

A palette and type pairing with contrast evidence and shared tokens.

## Pitfalls

- Contrast is measured, not eyeballed; run the numbers.
- Decorative low-contrast text still excludes readers.

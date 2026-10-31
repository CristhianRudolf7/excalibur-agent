---
name: polymarket-search
description: Consult Polymarket prediction markets and their evolution.
metadata:
  excalibur:
    group: browser
    version: 1.0.0
    platforms:
    - linux
    tools:
    - source_search
    - web_read
    display:
      name_en: Polymarket search
      name_es: Búsqueda en Polymarket
      desc_en: Consult Polymarket prediction markets and their evolution.
      desc_es: Consultar mercados de predicción y su evolución.
---
# Polymarket search

Use this skill to read crowd probabilities on future events. Market prices aggregate expectations with real stakes — useful context, never a decision-maker.

## When to use

- Base rates for an event forecast are needed.
- Tracking how expectations evolve over time.
- Calibrating a planning assumption.

## Procedure

1. Find the exact market matching the question; read its resolution rules first.
2. Record the current price and its history, not a single snapshot.
3. Check liquidity and volume: thin markets mislead.
4. Treat prices as aggregate opinion with incentives, not as truth.
5. Report rules, price, history, and liquidity together.

## Output

Market context with rules, prices, history, and liquidity caveats.

## Pitfalls

- Resolution rules decide payouts; read them first.
- Thin markets are noise with a price tag.

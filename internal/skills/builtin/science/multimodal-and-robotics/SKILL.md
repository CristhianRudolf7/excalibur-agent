---
name: multimodal-and-robotics
description: Research multimodal models and policies for robotics.
metadata:
  excalibur:
    group: science
    version: 1.0.0
    platforms:
    - linux
    tools:
    - vision_extract
    - audio_transcribe
    - exec
    display:
      name_en: Multimodal and robotics
      name_es: Multimodalidad y robótica
      desc_en: Research multimodal models and policies for robotics.
      desc_es: Investigar modelos multimodales y robótica.
---
# Multimodal and robotics

Use this skill where perception meets action. Grounded multimodal evaluation plus careful real-world testing keeps policies honest about what they can do.

## When to use

- Working with vision, audio, or embodied models.
- Evaluating multimodal capabilities.
- Testing robot policies.

## Procedure

1. Evaluate perception separately from action before combining.
2. Test in simulation with domain randomization, then in controlled reality.
3. Measure failure modes explicitly: what the policy does when unsure.
4. Keep humans in the loop for consequential actions.
5. Report the sim-to-real gap measured, not assumed.

## Output

Evaluated multimodal or robotics work with measured gaps.

## Pitfalls

- Simulation success is not real success; measure the gap.
- Consequential actions need human gates.

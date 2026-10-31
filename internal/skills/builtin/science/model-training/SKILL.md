---
name: model-training
description: Train and tune models, including distributed training and efficient tuning methods.
metadata:
  excalibur:
    group: science
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - gpu_training
    display:
      name_en: Model training
      name_es: Entrenamiento de modelos
      desc_en: Train and tune models, including distributed training and efficient tuning methods.
      desc_es: Entrenar y ajustar modelos, incluido distribuido.
---
# Model training

Use this skill to train reliably at any scale. Reproducible setups, monitored runs, and efficient tuning methods turn GPU hours into capable checkpoints.

## When to use

- Pretraining, fine-tuning, or adapting a model.
- Scaling training across devices.
- Choosing efficient tuning methods.

## Procedure

1. Fix data, code, and hyperparameter versions before launching.
2. Start with a small-scale shakedown proving the pipeline learns.
3. Monitor loss, gradients, and throughput; stop divergence early.
4. Use efficient methods (adapters, quantization-aware tuning) where full tuning wastes.
5. Checkpoint with evaluation scores, not just loss.

## Output

Trained checkpoints with setup versions, monitoring, and eval scores.

## Pitfalls

- Unmonitored runs burn budget silently; watch them.
- Shakedown first; full-scale debugging is the most expensive kind.

---
name: gpu-training-jobs
description: Configure the provider and GPU chosen by the user, launch trainings, follow their metrics
  and recover results.
metadata:
  excalibur:
    group: creative-tools
    version: 1.0.0
    platforms:
    - linux
    tools:
    - gpu_training
    display:
      name_en: GPU training jobs
      name_es: Entrenamientos en GPU
      desc_en: Configure the provider and GPU chosen by the user, launch trainings, follow their metrics
        and recover results.
      desc_es: Lanzar entrenamientos, seguir métricas y recuperar resultados.
---
# GPU training jobs

Use this skill to run training without babysitting the command line. Provider, GPU, and budget fixed by the user; launch, monitoring, and result recovery handled systematically.

## When to use

- Training or fine-tuning on GPUs.
- The user chose a provider and GPU type.
- Long runs need monitoring and recovery.

## Procedure

1. Confirm provider, GPU type, image, and budget with the user first.
2. Launch with versioned code, data, and hyperparameters recorded.
3. Monitor metrics and costs; stop diverging or over-budget runs early.
4. Recover checkpoints and results to versioned storage on completion.
5. Report metrics, cost, and reproduction info together.

## Output

Trained artifacts with metrics, cost, and reproduction records.

## Pitfalls

- Unbudgeted runs surprise; confirm cost guards first.
- Unversioned launches cannot be reproduced; record everything.

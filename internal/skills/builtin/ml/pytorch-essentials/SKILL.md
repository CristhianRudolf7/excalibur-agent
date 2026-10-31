---
name: pytorch-essentials
description: "Train and evaluate PyTorch models with reproducible loops, checkpointing and device handling."
metadata:
  excalibur:
    group: ml
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: PyTorch essentials
      name_es: Esenciales de PyTorch
      desc_en: "Train and evaluate PyTorch models with reproducible loops, checkpointing and device handling."
      desc_es: "Entrenar y evaluar modelos PyTorch con loops reproducibles."
---
# PyTorch essentials

Use this skill for PyTorch training and evaluation. Deterministic setup plus a logged loop turns GPU hours into comparable experiments.

## When to use

- Training, fine-tuning or evaluating a `torch.nn.Module`.
- Nondeterministic results or device (`cuda` vs `cpu`) errors.
- Saving and resuming runs with optimizer state.

## Procedure

1. Setup: `torch.manual_seed(0)`, `torch.backends.cudnn.deterministic = True` where affordable, log `torch.__version__` and `torch.cuda.is_available()`; pick one device and move model AND batches to it.
2. Loop: `model.train()` + zero_grad + backward + step for training, `model.eval()` + `torch.no_grad()` for evaluation; never mix modes.
3. Checkpoint dicts with model, optimizer, scheduler and epoch (`torch.save({...})`); resume by loading all four, not just weights.
4. Batch safely: start small, scale up watching `nvidia-smi`; use gradient accumulation when memory caps the batch.
5. Evaluate on a held-out split with the same preprocessing as training; report loss AND the task metric.

## Pitfalls

- Forgetting `model.eval()` leaves dropout/BatchNorm in training mode and silently degrades eval scores.
- `.item()`/`.cpu()` syncs kill throughput in tight loops; accumulate on-device and sync per batch.
- Pickled checkpoints from untrusted sources execute code on load; only load your own or verified files (weights-only loaders when available).

## Verification

- Loss decreases on a tiny overfit-batch sanity run; eval metric reproduces within noise across two seeds; checkpoint resume continues the loss curve.

---
name: huggingface-transformers
description: "Load and run Hugging Face models offline-aware: tokenizers, pipelines, caching and pinning."
metadata:
  excalibur:
    group: ml
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Hugging Face transformers
      name_es: Transformers de Hugging Face
      desc_en: "Load and run Hugging Face models offline-aware: tokenizers, pipelines, caching and pinning."
      desc_es: "Modelos Hugging Face con caché offline, tokenizers y pins."
---
# Hugging Face transformers

Use this skill to run pretrained models from the Hub. Pin revisions, warm the cache while online, and always pair the model with its own tokenizer.

## When to use

- Inference or fine-tuning with `transformers`, `datasets` or `diffusers`.
- `HF` downloads failing offline or versions shifting between runs.
- Picking a checkpoint for a task (size vs quality vs license).

## Procedure

1. Pin everything: `model = AutoModel.from_pretrained("org/name", revision="<sha>")` with the matching `AutoTokenizer`; record id + revision.
2. Warm the cache online: `huggingface-cli download org/name --revision <sha>`; offline runs set `HF_HUB_OFFLINE=1` and reuse `~/.cache/huggingface`.
3. Batch and device: `pipeline("text-classification", model=..., device=0)` on GPU, batch inputs, `torch.no_grad()` around inference loops.
4. Evaluate on YOUR data with YOUR metric; Hub leaderboard numbers do not transfer across domains.
5. Check the license and `config.json` (max length, expected preprocessing) before shipping anything built on the weights.

## Pitfalls

- A tokenizer from another checkpoint silently shifts all predictions; model and tokenizer revisions must match.
- `trust_remote_code=True` executes repository code; enable only for reviewed repos.
- Default `pipeline` batching is size-1; throughput work needs explicit batching and padding to the longest (or a fixed max length).

## Verification

- Offline re-run (`HF_HUB_OFFLINE=1`) reproduces the online outputs; the revision SHA is recorded; eval metric computed on held-out data.

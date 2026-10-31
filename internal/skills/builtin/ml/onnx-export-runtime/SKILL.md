---
name: onnx-export-runtime
description: "Export models to ONNX, verify numeric parity, and run them with onnxruntime."
metadata:
  excalibur:
    group: ml
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: ONNX export and runtime
      name_es: Exportación y runtime ONNX
      desc_en: "Export models to ONNX, verify numeric parity, and run them with onnxruntime."
      desc_es: "Exportar modelos a ONNX, verificar paridad y ejecutar con onnxruntime."
---
# ONNX export and runtime

Use this skill to ship models outside their training framework. Export once, verify parity numerically, then serve the portable artifact.

## When to use

- Deploying a PyTorch/sklearn model to CPU servers or edge runtimes.
- Framework version skew between training and serving.
- Speeding up inference without rewriting the model.

## Procedure

1. Export with fixed shapes first: `torch.onnx.export(model, dummy_input, "m.onnx", input_names=[...], output_names=[...], dynamo=True)` (or `tf2onnx` / `skl2onnx` per framework); record the opset version.
2. Check the graph: `onnx.checker.check_model("m.onnx")` and `onnx.shape_inference.infer_shapes`; inspect inputs/outputs with `onnxruntime.InferenceSession("m.onnx").get_inputs()`.
3. Verify parity: same inputs through both frameworks, compare with `np.allclose(rt_out, torch_out, rtol=1e-4, atol=1e-5)`; investigate any mismatch before serving.
4. Run with onnxruntime: `sess.run(None, {in_name: array})`; pick providers (`CUDAExecutionProvider` vs `CPUExecutionProvider`) and benchmark both.
5. Version the artifact: model file + opset + parity report travel together; re-verify on every re-export.

## Pitfalls

- Dynamic control flow and data-dependent shapes often fail export; trace with representative inputs and simplify first.
- Opset upgrades change operator semantics; pin the opset and the exporter version.
- Parity on one input is not proof; test batches, edge lengths and (for NLP) padding variants.

## Verification

- Checker passes, parity holds within tolerance on a validation batch, and the served p99 latency meets the budget.

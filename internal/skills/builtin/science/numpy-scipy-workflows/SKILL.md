---
name: numpy-scipy-workflows
description: "Compute with NumPy and SciPy: vectorized arrays, broadcasting, linear algebra and I/O."
metadata:
  excalibur:
    group: science
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: NumPy and SciPy workflows
      name_es: Flujos NumPy y SciPy
      desc_en: "Compute with NumPy and SciPy: vectorized arrays, broadcasting, linear algebra and I/O."
      desc_es: "Cálculo con NumPy y SciPy: arrays, broadcasting y álgebra lineal."
---
# NumPy and SciPy workflows

Use this skill for numerical work in Python. Vectorize the hot path, verify shapes at boundaries, and test against a slow-but-obvious reference.

## When to use

- Array math, statistics, signal processing or optimization in Python.
- Slow Python loops over numeric data.
- Reading or writing `.npy`/`.npz` and MATLAB `.mat` files.

## Procedure

1. Setup: pinned `numpy`/`scipy` in a venv (see `python-pip-uv-venv`); print `numpy.__version__` and `numpy.show_config()` for BLAS info on perf work.
2. Vectorize: replace loops with ufuncs and broadcasting; assert shapes (`assert a.shape == (n, m)`) at function boundaries.
3. Use the right routine: `scipy.linalg` over `numpy.linalg` for factorizations, `scipy.optimize.minimize` for fits, `scipy.signal` for filters, `scipy.sparse` when density < 10%.
4. Persist with `np.save`/`np.load` (arrays) or `savemat`/`loadmat` (MATLAB interop); store units and shapes alongside results.
5. Test numerics with `np.allclose`/`np.testing.assert_allclose` tolerances, never exact equality on floats.

## Pitfalls

- Broadcasting silently stretches `(n,)` against `(n,m)`; a missing axis is a wrong answer, not an error — assert shapes.
- `np.linalg` on huge dense matrices OOMs; check memory order-of-magnitude before factoring.
- NaN poisons silently through reductions; `np.isnan().any()` checks belong in validation code.

## Verification

- Fast version matches the naive loop version within tolerance on random inputs; shapes asserted; timing shows the expected speedup.

---
name: numerical-methods-practice
description: "Solve equations, integrate and fit data with stable numerical methods and error budgets."
metadata:
  excalibur:
    group: science
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Numerical methods practice
      name_es: Métodos numéricos en práctica
      desc_en: "Solve equations, integrate and fit data with stable numerical methods and error budgets."
      desc_es: "Ecuaciones, integración y ajuste con métodos estables y presupuesto de error."
---
# Numerical methods practice

Use this skill when code must compute an answer approximately: roots, integrals, ODEs, fits. Stability and error budgets come before cleverness.

## When to use

- Solving nonlinear equations, quadrature, interpolation or curve fitting.
- Results that depend on step sizes, tolerances or initial guesses.
- Validating a numerical result against analysis or data.

## Procedure

1. State the error budget first: what tolerance proves the task done (1e-3? 1e-9?); pick the method to match, not the fanciest available.
2. Prefer library routines: bracketing root finders over Newton when derivatives are iffy, adaptive quadrature (`scipy.integrate.quad`) over fixed rules, `least_squares` with bounds for fits.
3. Scale and condition: normalize units so values sit near 1; check condition numbers before trusting inversions; prefer solving `Ax=b` over explicit inverses.
4. Test convergence: halve the step (or tighten `tol`) and confirm the answer moves less than the budget; test edge cases (stiff ODEs, singular points) separately.
5. Validate independently: an analytic special case, a conserved quantity, or a second method must agree within budget.

## Pitfalls

- Fixed-step integration of stiff or singular problems diverges while looking productive; use adaptive or stiff-aware solvers.
- Overfitting a high-degree interpolant oscillates (Runge); prefer splines or least-squares fits for noisy data.
- Float equality in stopping criteria stalls or loops forever; stop on tolerances, not `==`.

## Verification

- Mesh/tolerance refinement changes the answer less than the budget; an independent check agrees; the error budget is written next to the result.

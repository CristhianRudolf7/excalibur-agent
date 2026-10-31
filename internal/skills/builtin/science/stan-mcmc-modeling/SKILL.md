---
name: stan-mcmc-modeling
description: "Fit Bayesian models with Stan or MCMC: model checks, divergences, R-hat and posterior validation."
metadata:
  excalibur:
    group: science
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Stan and MCMC modeling
      name_es: Modelado Stan y MCMC
      desc_en: "Fit Bayesian models with Stan or MCMC: model checks, divergences, R-hat and posterior validation."
      desc_es: "Modelos bayesianos con Stan/MCMC: divergencias, R-hat y validación."
---
# Stan and MCMC modeling

Use this skill for Bayesian inference with Stan, PyMC or similar. The sampler's diagnostics are the verdict: no divergent transitions, R-hat near 1, and effective samples to spare.

## When to use

- Fitting hierarchical, measurement-error or ODE-embedded models.
- Sampler warnings: divergences, max treedepth, low BFMI or low n_eff.
- Deciding whether the posterior (not just the point estimate) answers the question.

## Procedure

1. Start non-centered and weakly informative: non-centered parameterizations for hierarchies, finite priors (never `uniform(-inf, inf)` by accident).
2. Fit short first: few iterations on a data subset to catch specification bugs fast; simulate fake data from the prior and recover the parameters before touching real data.
3. Read diagnostics as gates: zero post-warmup divergences, `Rhat < 1.01`, `n_eff` in the hundreds per chain; treedepth saturation means reparameterize, not just raise the cap.
4. Validate the model: posterior predictive checks against the observed data shape; LOO-CV (`loo` package) to compare candidate models.
5. Report posteriors, not points: medians with credible intervals, and the exact model code + seed + version alongside every figure.

## Pitfalls

- Ignoring divergences biases everything downstream; a "converged" model with divergences is a wrong model.
- Centered hierarchies funnel and diverge; non-center the varying effects as the default.
- Comparing models by in-sample fit rewards complexity; use predictive criteria (LOO/WAIC) instead.

## Verification

- All diagnostics green on the final fit; prior recovery and posterior predictive checks documented; results reproduce from the recorded seed.

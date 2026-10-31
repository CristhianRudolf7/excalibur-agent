---
name: deployment-platforms
description: 'Deploy and diagnose on platforms like Vercel and similar: preview environments, variables,
  domains and logs.'
metadata:
  excalibur:
    group: software-engineering
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - web_search
    display:
      name_en: Deployment platforms
      name_es: Plataformas de despliegue
      desc_en: 'Deploy and diagnose on platforms like Vercel and similar: preview environments, variables,
        domains and logs.'
      desc_es: Desplegar y diagnosticar en plataformas como Vercel.
---
# Deployment platforms

Use this skill for platform-hosted deployments. Preview environments, environment variables, domains, and logs behave per-platform; knowing the specific knobs beats generic deploy lore.

## When to use

- Deploying to or debugging a hosting platform.
- Preview environments, variables, or domains misbehave.
- Choosing between platform options.

## Procedure

1. Confirm the platform's build settings: command, output dir, runtime versions.
2. Manage variables per environment; secrets via the vault, never in dashboard screenshots.
3. Verify domains, certificates, and routing before announcing.
4. Use preview deployments for every change; read the platform logs first on failure.
5. Record the working configuration where the team can find it.

## Output

A working deployment with verified previews, variables, domains, and logs.

## Pitfalls

- Production variables do not belong in previews.
- Read the platform log before theorizing.

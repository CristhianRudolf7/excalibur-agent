---
name: web-security-review
description: 'Review web application security: injections, XSS, CSRF, access control, configuration.'
metadata:
  excalibur:
    group: security
    version: 1.0.0
    platforms:
    - linux
    tools:
    - browser_devtools
    - exec
    - grep
    display:
      name_en: Web security review
      name_es: Revisión de seguridad web
      desc_en: 'Review web application security: injections, XSS, CSRF, access control, configuration.'
      desc_es: Revisar inyecciones, XSS, CSRF y control de acceso.
---
# Web security review

Use this skill for web-facing code and configuration. The classic classes — injection, XSS, CSRF, broken access control, misconfiguration — still cause most web incidents, and each has a concrete check.

## When to use

- Reviewing web handlers, templates, or APIs.
- Shipping authentication-adjacent or input-heavy features.
- Configuring headers, cookies, or CORS.

## Procedure

1. Trace untrusted input from request to sink: queries, commands, templates, redirects.
2. Check output encoding per context; test stored and reflected paths with canaries.
3. Verify CSRF protection on state-changing endpoints and cookie flags.
4. Test access control per object and per role, including direct-object references.
5. Review security headers, CORS, and framework security defaults.

## Output

Web findings with reproduction, severity, and fix per class.

## Pitfalls

- Framework defaults are not guarantees; verify each control.
- Test authorization as the attacker role, not as admin.

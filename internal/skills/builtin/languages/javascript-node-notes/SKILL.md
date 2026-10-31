---
name: javascript-node-notes
description: "Run and fix JavaScript and Node.js: ESM vs CJS, async errors, npm scripts and strict lint."
metadata:
  excalibur:
    group: languages
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: JavaScript and Node.js notes
      name_es: Notas de JavaScript y Node.js
      desc_en: "Run and fix JavaScript and Node.js: ESM vs CJS, async errors, npm scripts and strict lint."
      desc_es: "Ejecutar y corregir JS/Node: ESM vs CJS, async, scripts y lint."
---
# JavaScript and Node.js notes

Use this skill on JS/TS and Node projects. Module format and async error flow cause most confusion; pin both down first.

## When to use

- `SyntaxError: Cannot use import statement` or `require is not defined` (module-format mismatch).
- Unhandled rejections, swallowed async errors, or flaky timing.
- Running scripts, tests or linters in a Node project.

## Procedure

1. Identify the module system: `"type": "module"` means ESM (`import`); otherwise CommonJS (`require`). Keep extensions explicit in ESM (`./x.js`).
2. Run via the project: `npm run <script>` / `npm test` so local binaries and env apply (see `node-npm-packaging`).
3. Trace async errors with `node --trace-uncaught` / `--unhandled-rejections=strict`; await every promise at boundaries and never mix callbacks with promises in new code.
4. Lint and format with the project's config (`eslint`, `prettier --check .`); fix lint errors rather than disabling rules.
5. Pin the runtime: honor `.nvmrc`/`engines`; `node --version` must match before debugging anything else.

## Pitfalls

- Top-level await works in ESM only; CJS needs an async wrapper.
- `==` coercion, `NaN !== NaN`, and `0.1 + 0.2` are classic traps; use `===`, `Number.isNaN`, and integer cents for money.
- `node_modules` staleness mimics code bugs; `npm ci` fresh before blaming the code.

## Verification

- `npm test` and the linter pass; the failing script now exits 0 with the expected output on the pinned Node version.

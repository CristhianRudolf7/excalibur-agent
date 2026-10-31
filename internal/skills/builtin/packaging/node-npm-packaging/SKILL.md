---
name: node-npm-packaging
description: "Install and pin Node.js dependencies with npm, including offline caches and lockfiles."
metadata:
  excalibur:
    group: packaging
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Node.js packaging with npm
      name_es: Empaquetado Node.js con npm
      desc_en: "Install and pin Node.js dependencies with npm, including offline caches and lockfiles."
      desc_es: "Dependencias Node.js con npm, lockfiles y cachés sin red."
---
# Node.js packaging with npm

Use this skill for any Node.js project: respect the lockfile, install reproducibly, and keep an offline path through the npm cache.

## When to use

- A `package.json` / `package-lock.json` project must build or test.
- `npm install` changes versions or takes too long.
- No network but a warm npm cache or tarball directory exists.

## Procedure

1. Check the toolchain: `node --version && npm --version`; match the project's `.nvmrc` or `engines` field (use `nvm`/`volta` if available).
2. Reproducible install: `npm ci` (uses `package-lock.json` exactly, starts from a clean `node_modules`).
3. Add dependencies deliberately: `npm install --save-exact <pkg>` for apps; never hand-edit the lockfile.
4. Offline: prefill with `npm install --prefer-offline` on a connected machine, then copy `~/.npm` and run `npm ci --offline` or `--prefer-offline` at the target.
5. Run scripts via `npm run <script>` or `npx <bin>`; local `.bin` wins over global installs.

## Pitfalls

- `npm install` (without `ci`) rewrites the lockfile and can upgrade transitives; CI and eval runs must use `npm ci`.
- Global installs (`npm i -g`) are invisible to the project and break on other machines; keep tools in `devDependencies`.
- Native modules need a toolchain (`build-essential`, `python3`); see `apt-offline-installs` and `gcc-clang-toolchain`.

## Verification

- `npm ls --omit=dev` exits 0 (no unmet or invalid deps) and `npm run <build|test>` passes.

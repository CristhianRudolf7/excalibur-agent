---
name: regex-practical
description: "Write correct grep-compatible regular expressions for searching code and logs."
metadata:
  excalibur:
    group: terminal
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    - grep
    display:
      name_en: Practical regular expressions
      name_es: Expresiones regulares prácticas
      desc_en: "Write correct grep-compatible regular expressions for searching code and logs."
      desc_es: "Expresiones regulares correctas para buscar en código y logs."
---
# Practical regular expressions

Use this skill when searching code or logs with patterns. Prefer fixed strings by default; reach for regex only for shape matching (identifiers, versions, addresses).

## When to use

- Finding error shapes in logs (`ERROR [0-9]{4}-..-..`).
- Locating code patterns (function definitions, imports, TODOs with owners).
- Validating quick formats before writing a parser.

## Procedure

1. Start literal: `grep -rn -F 'Connection refused' .` (`-F` disables regex metacharacters).
2. Escalate to extended regex with `grep -rEn`: `grep -rEn 'def (test_|bench_)[a-z_]+\(' --include='*.py' .`.
3. Anchor to cut noise: `^` start, `$` end, `\b` word boundary; use character classes `[0-9a-f]`, shorthands only with `-P` (PCRE): `\d`, `\w`, `\s`.
4. Quantify carefully: `*` (zero+), `+` (one+), `?` (optional), `{2,4}` (range); make greedy matches lazy-safe by excluding the delimiter (`[^"]*` instead of `.*`).
5. Test the pattern against both a positive and a negative example before running it over the tree.

## Pitfalls

- Basic `grep` (BRE) needs `\(...\)` for groups; use `grep -E` (ERE) for `(...)`, `+`, `?`, `|`.
- `.` matches any char: escape literal dots in versions and IPs (`1\.2\.3`).
- Never parse nested formats (HTML, JSON strings with escapes) with regex; use a real parser (see `jq-recipes`).

## Verification

- The pattern matches the known-positive line and rejects a near-miss line.
- Count hits before acting on them (`| wc -l`); a surprise count means the pattern is wrong.

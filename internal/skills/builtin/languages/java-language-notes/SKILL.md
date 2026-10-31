---
name: java-language-notes
description: "Build and fix Java with javac, jars, Maven or Gradle, and read stack traces precisely."
metadata:
  excalibur:
    group: languages
    version: 1.0.0
    platforms:
    - linux
    tools:
    - exec
    display:
      name_en: Java language notes
      name_es: Notas del lenguaje Java
      desc_en: "Build and fix Java with javac, jars, Maven or Gradle, and read stack traces precisely."
      desc_es: "Compilar y corregir Java con javac, jars, Maven o Gradle."
---
# Java language notes

Use this skill on Java codebases. The build tool (Maven or Gradle) owns the classpath; stack traces name the exact line to start from.

## When to use

- Compiling, testing or debugging Java sources.
- `ClassNotFoundException`, `NoSuchMethodError` (classpath/version skew) or NPEs.
- Choosing collections, streams or concurrency primitives.

## Procedure

1. Check `java -version` and match the project's release (`maven.compiler.release` / Gradle `sourceCompatibility`); mixed JDKs cause class-file-version errors.
2. Build with the wrapper when present: `./mvnw -q -DskipTests package` then `./mvnw -q test`, or `./gradlew build`; never hand-assemble classpaths the build already manages.
3. Read stack traces top-down: first application frame (not `java.base` internals) is the bug site; reproduce with a minimal `@Test` before fixing.
4. Manage deps in `pom.xml`/`build.gradle` with pinned versions; resolve conflicts with `mvn dependency:tree` or `gradle dependencies`.
5. Prefer `List`/`Map` interfaces, immutable copies (`List.copyOf`), `Optional` for absent returns, and `java.util.concurrent` over raw threads.

## Pitfalls

- `NoSuchMethodError` at runtime means compile-time and runtime jars differ; align versions, never shade blindly.
- Checked exceptions force handling design; wrap in unchecked only at deliberate boundaries.
- Encoding is UTF-8 by declaration, not by luck: set `project.build.sourceEncoding` and file encodings explicitly.

## Verification

- Full `./mvnw test` or `./gradlew build` green on a clean checkout; the reproducer test fails before and passes after the fix.

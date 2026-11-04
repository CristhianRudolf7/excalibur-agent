export interface TestSummary {
  runner: string
  passed?: number
  failed?: number
  unit: 'tests' | 'packages'
}
/** Summaries describe observed runner output; an exit code alone never invents counts. */
export function testSummary(command: string, output = ''): TestSummary | undefined {
  // oxlint-disable-next-line no-control-regex -- Strip terminal ANSI color sequences before parsing runner summaries.
  const clean = output.replace(/\u001b\[[0-9;]*[A-Za-z]/g, '')
  const runner = /\bgo\s+test\b/.test(command)
    ? 'Go'
    : /\b(?:pytest|py\.test)\b/.test(command)
      ? 'pytest'
      : /\bvitest\b/.test(command)
        ? 'Vitest'
        : /\bjest\b/.test(command)
          ? 'Jest'
          : /\bcargo\s+test\b/.test(command)
            ? 'Cargo'
            : /\b(?:npm|pnpm|yarn)\s+(?:run\s+)?test(?:\b|:)/.test(command)
              ? 'test'
              : undefined
  if (!runner) return
  const result: TestSummary = { runner, unit: 'tests' }
  if (runner === 'Go') {
    const passed = clean.match(/^--- PASS:/gm)?.length ?? 0
    const failed = clean.match(/^--- FAIL:/gm)?.length ?? 0
    if (passed || failed) return { ...result, passed, failed }
    const packagesPassed = clean.match(/^ok\s+\S+/gm)?.length ?? 0
    const failures = clean.split('\n').filter((line) => /^FAIL\s+\S+/.test(line)).length
    if (packagesPassed || failures)
      return { ...result, unit: 'packages', passed: packagesPassed, failed: failures }
    return result
  }
  const summaries =
    runner === 'Cargo'
      ? clean.split('\n').filter((line) => line.includes('test result:'))
      : runner === 'pytest'
        ? clean
            .split('\n')
            .filter((line) => /\d+ (?:passed|failed)/.test(line))
            .slice(-1)
        : clean
            .split('\n')
            .filter((line) => /^\s*Tests\s*[: ]/.test(line))
            .slice(-1)
  // npm/pnpm may invoke any of the common runners.
  const lines = summaries.length
    ? summaries
    : clean
        .split('\n')
        .filter((line) => /(?:test result:|=+ .*\d+ (?:passed|failed))/.test(line))
        .slice(-1)
  if (lines.length) {
    result.passed = 0
    result.failed = 0
    for (const line of lines) {
      result.passed += Number(line.match(/(\d+)\s+passed/)?.[1] ?? 0)
      result.failed += Number(line.match(/(\d+)\s+failed/)?.[1] ?? 0)
    }
  }
  return result
}

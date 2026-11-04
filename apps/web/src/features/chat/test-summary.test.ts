import { expect, it } from 'vitest'
import { testSummary } from './test-summary'

it.each([
  ['go test -v ./...', '--- PASS: TestOne (0.00s)\n--- FAIL: TestTwo (0.00s)', 'Go', 1, 1, 'tests'],
  [
    'go test ./...',
    'ok  example/a 0.01s\nFAIL example/b [build failed]\nFAIL',
    'Go',
    1,
    1,
    'packages',
  ],
  ['python -m pytest -q', '=== 2 failed, 18 passed in 0.50s ===', 'pytest', 18, 2, 'tests'],
  [
    'pnpm vitest run',
    'Test Files 1 failed | 2 passed (3)\n     Tests  2 failed | 10 passed (12)',
    'Vitest',
    10,
    2,
    'tests',
  ],
  [
    'npx jest',
    'Test Suites: 1 failed, 2 passed, 3 total\nTests: 3 failed, 12 passed, 15 total',
    'Jest',
    12,
    3,
    'tests',
  ],
  ['npm test', 'Tests: 5 passed, 5 total', 'test', 5, 0, 'tests'],
  ['pnpm run test:unit', 'Tests  1 failed | 9 passed (10)', 'test', 9, 1, 'tests'],
  [
    'cargo test',
    'test result: ok. 3 passed; 0 failed; 1 ignored\ntest result: FAILED. 2 passed; 1 failed;',
    'Cargo',
    5,
    1,
    'tests',
  ],
] as const)(
  'detects %s without counting suites as tests',
  (command, output, runner, passed, failed, unit) => {
    expect(testSummary(command, output)).toEqual({ runner, passed, failed, unit })
  },
)
it('does not invent counts or recognize ordinary commands as tests', () => {
  expect(testSummary('pnpm test', 'still compiling')).toEqual({ runner: 'test', unit: 'tests' })
  expect(testSummary('cat test.log', '3 passed')).toBeUndefined()
  expect(testSummary('pytest', '\u001b[32m2 passed\u001b[0m')).toMatchObject({
    passed: 2,
    failed: 0,
  })
})

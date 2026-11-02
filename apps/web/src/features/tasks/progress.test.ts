import { expect, it } from 'vitest'
import { criteriaProgress } from './format'
it('clamps progress and handles tasks without criteria', () => {
  expect(criteriaProgress({ criteria_met: 1, criteria_total: 4 })).toBe(25)
  expect(criteriaProgress({ criteria_met: 0, criteria_total: 0 })).toBe(0)
  expect(criteriaProgress({ criteria_met: 8, criteria_total: 4 })).toBe(100)
  expect(criteriaProgress({ criteria_met: -1, criteria_total: 4 })).toBe(0)
})

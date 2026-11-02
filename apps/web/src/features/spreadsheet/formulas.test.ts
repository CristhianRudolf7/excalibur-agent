import { describe, it, expect } from 'vitest'
import { evaluateCell, coordinates, cellAddress } from './formulas'
import { getBudgetData } from './mock-data'
describe('formula engine', () => {
  it.each([
    ['=1+2*3', 7],
    ['=(1+2)*3', 9],
    ['=-3+4', 1],
    ['=2^3^2', 512],
    ['=SUM(A1:A3)', 12],
    ['=AVERAGE(A1:A3)', 4],
    ['=MIN(A1:A3)', 2],
    ['=MAX(A1:A3)', 6],
    ['=COUNT(A1:A3)', 3],
    ['=COUNT(C1:C3)', 0],
    ['=AVERAGE(A1:A4)', 4],
    ['=ROUND(1.85625,2)', 1.86],
    ['=IF(A1>1,"yes","no")', 'yes'],
    ['=IF(FALSE,1/0,5)', 5],
    ['=IF(TRUE,IF(FALSE,0,3),1/0)', 3],
    ['=1/0', '#DIV/0!'],
    ['=A101', '#REF!'],
    ['=AA1', '#REF!'],
    ['=MISSING(1)', '#NAME?'],
    ['=SUM(A1:A101)', '#REF!'],
    ['=1+', '#NAME?'],
    ['=UNKNOWN', '#NAME?'],
  ] as const)('%s evaluates to %s', (formula, result) => {
    expect(evaluateCell({ A1: 2, A2: 4, A3: 6, B1: formula }, 'B1')).toBe(result)
  })
  it('detects cycles and propagates errors', () => {
    expect(evaluateCell({ A1: '=B1', B1: '=C1', C1: '=A1' }, 'A1')).toBe('#CIRC!')
    expect(evaluateCell({ A1: '=1/0', B1: '=A1+2' }, 'B1')).toBe('#DIV/0!')
  })
  it('calculates the reference budget and recalculates dependent formulas', () => {
    const cells = getBudgetData()
    expect(evaluateCell(cells, 'H4')).toBe(1856.25)
    expect(evaluateCell(cells, 'F8')).toBe(11075)
    expect(evaluateCell(cells, 'H8')).toBe(7922.25)
    cells.C2 = 121
    expect(evaluateCell(cells, 'F2')).toBe(2299)
    expect(evaluateCell(cells, 'H2')).toBe(1615)
    expect(evaluateCell(cells, 'H8')).toBe(7941.25)
  })
  it('handles blanks, text, absolute references and malformed coordinates', () => {
    expect(evaluateCell({ A1: '=SUM(B1:B3)', B1: 2, B2: 'text' }, 'A1')).toBe(2)
    expect(evaluateCell({ A1: 3, B1: '=$A$1*2' }, 'B1')).toBe(6)
    expect(coordinates('H4')).toEqual([3, 7])
    expect(coordinates('invalid')).toEqual([-1, -1])
  })
})

it('addresses columns beyond Z in imported workbooks', () => {
  expect(cellAddress(3, 26)).toBe('AA4')
  expect(cellAddress(0, 16383)).toBe('XFD1')
})

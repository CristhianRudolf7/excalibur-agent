import type { Cells } from './formulas'
export const translatedCells: Record<string, string> = {
  A1: 'concept',
  B1: 'category',
  C1: 'units',
  D1: 'price',
  E1: 'discount',
  F1: 'revenue',
  G1: 'cost',
  H1: 'margin',
  A2: 'basicPlan',
  A3: 'proPlan',
  A4: 'teamPlan',
  A5: 'consulting',
  A6: 'training',
  B2: 'subscription',
  B3: 'subscription',
  B4: 'subscription',
  B5: 'services',
  B6: 'services',
  A8: 'total',
}
export function getBudgetData(): Cells {
  const cells: Cells = {
    C2: 120,
    D2: 19,
    E2: 0,
    G2: 684,
    C3: 80,
    D3: 49,
    E3: 0,
    G3: 980,
    C4: 25,
    D4: 99,
    E4: 0,
    G4: 618.75,
    C5: 10,
    D5: 150,
    E5: 0,
    G5: 600,
    C6: 12,
    D6: 75,
    E6: 0,
    G6: 270,
  }
  for (let row = 2; row <= 6; row++) {
    cells[`F${row}`] = `=C${row}*D${row}*(1-E${row})`
    cells[`H${row}`] = `=F${row}-G${row}`
  }
  for (const col of ['F', 'G', 'H']) cells[`${col}8`] = `=SUM(${col}2:${col}6)`
  return cells
}

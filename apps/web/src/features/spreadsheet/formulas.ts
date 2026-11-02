export type CellValue = string | number
export type Cells = Record<string, CellValue>
type Value = number | string | boolean | Value[]
class FormulaError extends Error {}
const fail = (message: string): never => {
  throw new FormulaError(message)
}
export function columnName(column: number) {
  let name = ''
  for (let n = column + 1; n > 0; n = Math.floor((n - 1) / 26))
    name = String.fromCharCode(65 + ((n - 1) % 26)) + name
  return name
}
export function cellAddress(row: number, column: number) {
  return `${columnName(column)}${row + 1}`
}
export function coordinates(address: string): [number, number] {
  const match = /^([A-Z]+)(\d+)$/i.exec(address)
  if (!match) return [-1, -1]
  const col =
    [...match[1]!.toUpperCase()].reduce((value, char) => value * 26 + char.charCodeAt(0) - 64, 0) -
    1
  return [Number(match[2]) - 1, col]
}
export function evaluateCell(
  cells: Cells,
  address: string,
  visiting = new Set<string>(),
): CellValue {
  const raw = cells[address] ?? ''
  if (typeof raw === 'number' || !raw.startsWith('=')) return raw
  if (visiting.has(address)) return '#CIRC!'
  const next = new Set(visiting).add(address)
  try {
    return StringOrNumber(
      new Parser(raw.slice(1), (ref) => {
        const [row, column] = coordinates(ref)
        if (row < 0 || row >= 100 || column < 0 || column >= 26) return fail('#REF!')
        const value = evaluateCell(cells, ref, next)
        if (typeof value === 'string' && value.startsWith('#')) return fail(value)
        return value
      }).parse(),
    )
  } catch (error) {
    return error instanceof FormulaError ? error.message : '#NAME?'
  }
}
function StringOrNumber(value: Value): CellValue {
  if (Array.isArray(value)) return fail('#REF!')
  if (typeof value === 'boolean') return value ? 'TRUE' : 'FALSE'
  if (typeof value === 'number' && !Number.isFinite(value)) return fail('#DIV/0!')
  return value
}
function numeric(value: Value): number {
  if (Array.isArray(value)) return fail('#REF!')
  const number = Number(value)
  return Number.isFinite(number) ? number : fail('#NAME?')
}
class Parser {
  private tokens: string[]
  private index = 0
  private resolve: (address: string) => Value
  constructor(source: string, resolve: (address: string) => Value) {
    this.resolve = resolve
    this.tokens =
      source.match(
        /"(?:[^"]|"")*"|(?:\d+(?:\.\d*)?|\.\d+)|\$?[A-Za-z_]+\$?\d*|<>|<=|>=|[+\-*/^(),:;=<>]|\S/g,
      ) ?? []
  }
  private peek() {
    return this.tokens[this.index]?.toUpperCase()
  }
  private take() {
    return this.tokens[this.index++] ?? fail('#NAME?')
  }
  private expect(token: string) {
    if (this.take() !== token) fail('#NAME?')
  }
  parse(): Value {
    const result = this.comparison()
    if (this.index !== this.tokens.length) fail('#NAME?')
    return result
  }
  private comparison(): Value {
    let left = this.add()
    while (['=', '<>', '<', '>', '<=', '>='].includes(this.peek() ?? '')) {
      const op = this.take()
      const right = this.add()
      const a = StringOrNumber(left),
        b = StringOrNumber(right)
      left =
        op === '='
          ? a === b
          : op === '<>'
            ? a !== b
            : op === '<'
              ? a < b
              : op === '>'
                ? a > b
                : op === '<='
                  ? a <= b
                  : a >= b
    }
    return left
  }
  private add(): Value {
    let value = this.multiply()
    while (this.peek() === '+' || this.peek() === '-') {
      const op = this.take()
      const right = this.multiply()
      value = op === '+' ? numeric(value) + numeric(right) : numeric(value) - numeric(right)
    }
    return value
  }
  private multiply(): Value {
    let value = this.power()
    while (this.peek() === '*' || this.peek() === '/') {
      const op = this.take()
      const right = numeric(this.power())
      if (op === '/' && right === 0) fail('#DIV/0!')
      value = op === '*' ? numeric(value) * right : numeric(value) / right
    }
    return value
  }
  private power(): Value {
    const left = this.unary()
    return this.peek() === '^' ? (this.take(), numeric(left) ** numeric(this.power())) : left
  }
  private unary(): Value {
    if (this.peek() === '-') {
      this.take()
      return -numeric(this.unary())
    }
    if (this.peek() === '+') {
      this.take()
      return numeric(this.unary())
    }
    return this.atom()
  }
  private atom(): Value {
    const token = this.take()
    if (token === '(') {
      const value = this.comparison()
      this.expect(')')
      return value
    }
    if (token.startsWith('"')) return token.slice(1, -1).replaceAll('""', '"')
    if (/^(?:\d|\.)/.test(token)) return Number(token)
    const name = token.toUpperCase().replaceAll('$', '')
    if (name === 'TRUE' || name === 'FALSE') return name === 'TRUE'
    if (/^[A-Z]+\d+$/.test(name)) {
      if (this.peek() !== ':') return this.resolve(name)
      this.take()
      const end = this.take().toUpperCase().replaceAll('$', '')
      const [r1, c1] = coordinates(name),
        [r2, c2] = coordinates(end)
      if (r1 < 0 || r2 < 0 || c1 < 0 || c2 < 0 || r1 >= 100 || r2 >= 100 || c1 >= 26 || c2 >= 26)
        return fail('#REF!')
      const values: Value[] = []
      for (let r = Math.min(r1, r2); r <= Math.max(r1, r2); r++)
        for (let c = Math.min(c1, c2); c <= Math.max(c1, c2); c++)
          values.push(this.resolve(cellAddress(r, c)))
      return values
    }
    if (this.peek() !== '(') return fail('#NAME?')
    this.take()
    // IF evaluates only the selected branch, allowing guards around error-producing expressions.
    if (name === 'IF') {
      const condition = this.comparison()
      if (![',', ';'].includes(this.take())) fail('#NAME?')
      const start = this.index
      let depth = 0
      let separator = -1
      while (this.index < this.tokens.length) {
        const current = this.tokens[this.index]!
        if (current === '(') depth++
        if (current === ')') {
          if (depth === 0) break
          depth--
        }
        if ((current === ',' || current === ';') && depth === 0) separator = this.index
        this.index++
      }
      const end = this.index
      this.expect(')')
      if (separator < 0) return fail('#NAME?')
      const branch = condition
        ? this.tokens.slice(start, separator)
        : this.tokens.slice(separator + 1, end)
      return new Parser(branch.join(' '), this.resolve).parse()
    }
    const args: Value[] = []
    if (this.peek() !== ')') {
      args.push(this.comparison())
      while (this.peek() === ',' || this.peek() === ';') {
        this.take()
        args.push(this.comparison())
      }
    }
    this.expect(')')
    const flat: (string | number | boolean)[] = []
    const flatten = (value: Value): void => {
      if (Array.isArray(value)) value.forEach(flatten)
      else flat.push(value)
    }
    args.forEach(flatten)
    const numbers = flat.filter((v) => typeof v === 'number')
    switch (name) {
      case 'SUM':
        return numbers.reduce((a, b) => a + b, 0)
      case 'COUNT':
        return numbers.length
      case 'AVERAGE':
        return numbers.length
          ? numbers.reduce((a, b) => a + b, 0) / numbers.length
          : fail('#DIV/0!')
      case 'MIN':
        return numbers.length ? Math.min(...numbers) : 0
      case 'MAX':
        return numbers.length ? Math.max(...numbers) : 0
      case 'ROUND': {
        const factor = 10 ** numeric(args[1] ?? 0)
        return Math.round((numeric(args[0] ?? 0) + Number.EPSILON) * factor) / factor
      }
      default:
        return fail('#NAME?')
    }
  }
}

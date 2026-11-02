import { expect, it } from 'vitest'
import { sanitizeSVG } from './sanitize'
const svg = (body: string) =>
  `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100">${body}</svg>`
it('removes executable content, foreign objects, event handlers and external references', () => {
  const result = sanitizeSVG(
    svg(
      `<script>alert(1)</script><foreignObject><div>unsafe</div></foreignObject><g onclick="alert(1)" onload="alert(1)"><path d="M0 0L10 10"/><use href="https://evil.test/a.svg#x"/><use href="data:image/svg+xml,unsafe"/><image href="https://evil.test/track"/><animate attributeName="href" to="https://evil.test"/></g>`,
    ),
  )
  expect(result).not.toMatch(/script|foreignObject|onclick|onload|https:|data:|animate|<image/)
  expect(result).toContain('d="M0 0L10 10"')
})
it('preserves local gradients, markers and references while stripping unsafe CSS', () => {
  const result = sanitizeSVG(
    svg(
      `<defs><linearGradient id="paint"><stop offset="0" stop-color="red"/></linearGradient></defs><style>.node{fill:red;stroke:url(https://evil.test/a)} .label{font-size:14px}</style><rect fill="url(#paint)" style="stroke:blue;fill:url(https://evil.test/x);behavior:expression(alert(1))"/><use href="#paint"/>`,
    ),
  )
  expect(result).toContain('fill="url(#paint)"')
  expect(result).toContain('href="#paint"')
  expect(result).toContain('stroke:blue')
  expect(result).toContain('.node{fill:red}')
  expect(result).not.toMatch(/https:|expression|behavior/)
})
it.each([
  '<svg><script></svg>',
  '<html/>',
  '<!DOCTYPE svg [<!ENTITY unsafe "secret">]><svg xmlns="http://www.w3.org/2000/svg">&unsafe;</svg>',
])('rejects malformed, non-SVG or entity-bearing documents', (source) => {
  expect(() => sanitizeSVG(source)).toThrow('Invalid SVG')
})
it('blocks CSS escapes, imports and external paint attributes', () => {
  const result = sanitizeSVG(
    svg(
      '<style>@import "https://evil.test";</style><path fill="url(//evil.test)" style="fill:u\\72l(//evil.test);stroke:black"/>',
    ),
  )
  expect(result).not.toMatch(/evil|import|\\72/)
  expect(result).toContain('stroke:black')
})

it('produces valid SVG that can be sanitized again for whiteboard import', () => {
  const sanitized = sanitizeSVG(svg('<rect width="20" height="20"/>'))
  expect(sanitizeSVG(sanitized)).toBe(sanitized)
  expect(sanitized.match(/xmlns=/g)).toHaveLength(1)
})

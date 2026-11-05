import { readFile } from 'node:fs/promises'
import assert from 'node:assert/strict'

/** Include generated marks and SVG labels, which a text-node audit cannot see. */
export async function tokenContrast() {
  const css = await readFile('src/styles/tokens.css', 'utf8')
  const themes = [...css.matchAll(/:root\[data-theme='(light|dark)'\]\s*\{([^}]+)\}/g)]
  const results: Record<string, number> = {}
  for (const [, theme, body] of themes) {
    const colors = Object.fromEntries(
      [...body.matchAll(/(--[\w-]+):\s*(#[\da-f]{6})/gi)].map(([, name, color]) => [name, color]),
    )
    const luminance = (color: string) =>
      color
        .slice(1)
        .match(/../g)!
        .map((byte) => parseInt(byte, 16) / 255)
        .map((value) => (value <= 0.04045 ? value / 12.92 : ((value + 0.055) / 1.055) ** 2.4))
        .reduce((sum, value, i) => sum + value * [0.2126, 0.7152, 0.0722][i], 0)
    const pairs = [
      ['--ink', '--bg'],
      ['--ink-2', '--bg'],
      ['--ink', '--surface'],
      ['--ink-2', '--surface'],
      ['--blue-text', '--bg'],
      ['--blue-text', '--surface'],
      ['--blue-text', '--blue-tint'],
      ['--ink', '--blue-tint'],
      ['--diff-del', '--bg'],
      ['--diff-add', '--bg'],
    ]
    for (const [foreground, background] of pairs) {
      const a = luminance(colors[foreground]),
        b = luminance(colors[background])
      const ratio = (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)
      results[`${theme}:${foreground}/${background}`] = Number(ratio.toFixed(3))
      assert(ratio >= 4.5, `${theme} ${foreground}/${background} must reach AA (${ratio})`)
    }
    // Diff borders remain clear on quiet fills; sign text uses the page fill.
    for (const foreground of ['--diff-add', '--diff-del']) {
      const a = luminance(colors[foreground]),
        b = luminance(colors['--surface'])
      assert((Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05) >= 3)
    }
    const a = luminance(colors['--on-blue']),
      b = luminance(colors['--blue-fill'])
    const largeText = (Math.max(a, b) + 0.05) / (Math.min(a, b) + 0.05)
    assert(largeText >= 3, 'White on active plates requires large bold labels')
    results[`${theme}:large-plate-label`] = Number(largeText.toFixed(3))
  }
  assert.equal(themes.length, 2)
  return results
}

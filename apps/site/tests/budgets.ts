import { readFileSync, readdirSync, statSync, mkdirSync, writeFileSync } from 'node:fs'
import { resolve, dirname, basename } from 'node:path'
import { gzipSync } from 'node:zlib'

const dist = resolve('dist')
const html = readFileSync(`${dist}/index.html`, 'utf8')
const assets = new Map<string, string>()
function walk(dir: string) {
  for (const name of readdirSync(dir)) {
    const file = resolve(dir, name)
    if (statSync(file).isDirectory()) walk(file)
    else assets.set(name, file)
  }
}
walk(dist)
const used = new Set<string>()
function visit(file: string) {
  if (used.has(file)) return
  used.add(file)
  const source = readFileSync(file, 'utf8')
  // Vite's static imports, dynamic imports and module-preload dependency map.
  for (const match of source.matchAll(/["'`]([^"'`\s<>]+\.(?:m?js|css))["'`]/g)) {
    const target = assets.get(basename(match[1]!)) ?? resolve(dirname(file), match[1]!)
    if (assets.has(basename(target))) visit(target)
  }
}
visit(`${dist}/index.html`)
const size = (extension: string) =>
  [...used]
    .filter((file) => file.endsWith(extension))
    .reduce((sum, file) => sum + gzipSync(readFileSync(file)).length, 0)
const inlineScripts = [...html.matchAll(/<script\b[^>]*>([\s\S]*?)<\/script>/g)]
  .map((match) => match[1]!.trim())
  .filter(Boolean)
  .join('\n')
const inlineJavaScriptGzip = inlineScripts ? gzipSync(inlineScripts).length : 0
const report = {
  externalJavaScriptGzip: size('.js'),
  inlineJavaScriptGzip,
  javascriptGzip: size('.js') + inlineJavaScriptGzip,
  cssGzip: size('.css'),
  htmlGzip: gzipSync(html).length,
  budgets: { javascriptGzip: 60 * 1024, cssGzip: 40 * 1024, htmlGzip: 60 * 1024 },
  assets: [...used].map((file) => file.replace(`${dist}/`, '')),
}
console.log(JSON.stringify(report, null, 2))
mkdirSync('test-results', { recursive: true })
writeFileSync('test-results/budgets.json', JSON.stringify(report, null, 2))
for (const key of ['javascriptGzip', 'cssGzip', 'htmlGzip'] as const)
  if (report[key] > report.budgets[key]) throw new Error(`${key} exceeds budget`)

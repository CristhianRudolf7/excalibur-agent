import assert from 'node:assert/strict'
import { existsSync, readdirSync, readFileSync } from 'node:fs'
import { dirname, join, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'
import test from 'node:test'
import { docsSidebar } from '../../src/docs-sidebar.mjs'

const site = resolve(dirname(fileURLToPath(import.meta.url)), '../..')
const content = join(site, 'src/content/docs')
const dist = join(site, 'dist')
const excluded = new Set(['index.md', 'index.mdx', 'style-guide.mdx'])
function walk(directory) {
  return readdirSync(directory, { withFileTypes: true }).flatMap((entry) => {
    const path = join(directory, entry.name)
    return entry.isDirectory() ? walk(path) : [path]
  })
}
function pages(locale) {
  const root = join(content, locale)
  return walk(root)
    .filter((path) => /\.mdx?$/.test(path) && !excluded.has(path.slice(root.length + 1)))
    .map((path) => path.slice(root.length + 1).replace(/\.mdx?$/, ''))
    .sort()
}
function source(locale, slug) {
  const base = join(content, locale, slug)
  return readFileSync(existsSync(`${base}.mdx`) ? `${base}.mdx` : `${base}.md`, 'utf8')
}
const english = pages('docs')
const spanish = pages('es/docs')
const nav = docsSidebar.flatMap((group) => group.items)
const decode = (text) =>
  text
    .replace(/&amp;/g, '&')
    .replace(/&quot;/g, '"')
    .replace(/&#39;/g, "'")

function builtTarget(url) {
  const path = decodeURIComponent(url.pathname)
  assert(!path.split('/').includes('..'), `Unsafe link: ${url}`)
  const target = join(dist, path)
  if (existsSync(target) && !path.endsWith('/')) {
    if (!readdirSafe(target)) return target
  }
  return join(target, 'index.html')
}
function readdirSafe(path) {
  try {
    readdirSync(path)
    return true
  } catch {
    return false
  }
}

test('every owned page has a Spanish counterpart and a translated sidebar group', () => {
  assert.equal(english.length, 22)
  assert.deepEqual(spanish, english)
  assert.deepEqual([...nav].sort(), english.map((slug) => `docs/${slug}`).sort())
  for (const group of docsSidebar) assert(group.translations.es, group.label)
})

test('public content has titles, bounded length, and locale-correct internal links', () => {
  for (const locale of ['docs', 'es/docs']) {
    for (const slug of english) {
      const text = source(locale, slug)
      assert.match(text, /^---\ntitle: .+\ndescription: .+\n---/)
      assert(!text.includes('—'), `${locale}/${slug}: em dash`)
      assert(!text.includes('/home/'), `${locale}/${slug}: private filesystem path`)
      const prose = text
        .replace(/^---[\s\S]*?---/, '')
        .replace(/```[\s\S]*?```/g, '')
        .replace(/^import .+$/gm, '')
        .replace(/<[^>]+>/g, '')
      const words = prose.trim().split(/\s+/).length
      assert(words >= 200 && words <= 500, `${locale}/${slug}: ${words} words`)
      for (const match of text.matchAll(/\]\((\/(?:es\/)?docs[^)\s]*)\)/g)) {
        const href = match[1]
        assert.equal(href.startsWith('/es/'), locale === 'es/docs', `${locale}/${slug}: ${href}`)
        const target = builtTarget(new URL(href, 'https://content.invalid'))
        assert(existsSync(target), `${locale}/${slug}: missing ${href}`)
      }
    }
  }
})

test('rendered documentation content links and fragments resolve in the production output', () => {
  let links = 0
  for (const root of ['docs', 'es/docs']) {
    for (const path of walk(join(dist, root)).filter((path) => path.endsWith('.html'))) {
      const page = readFileSync(path, 'utf8')
      const start = page.indexOf('<main')
      assert(start >= 0, `${path}: missing main landmark`)
      const html = page.slice(start, page.indexOf('</main>', start))
      const route = path.slice(dist.length).replace(/index\.html$/, '')
      for (const match of html.matchAll(/<a\b[^>]*\bhref="([^"]*)"/g)) {
        const href = decode(match[1])
        if (!href || /^(https?:|mailto:|tel:|javascript:|data:)/.test(href)) continue
        const url = new URL(href, `https://content.invalid${route}`)
        const target = builtTarget(url)
        assert(existsSync(target), `${route}: missing ${href}`)
        if (url.hash) {
          const fragment = decodeURIComponent(url.hash.slice(1))
          const targetHtml = readFileSync(target, 'utf8')
          const ids = [...targetHtml.matchAll(/\bid="([^"]+)"/g)].map((item) => decode(item[1]))
          assert(ids.includes(fragment), `${route}: missing fragment ${href}`)
        }
        links++
      }
    }
  }
  assert(links > 0)
  console.log(`Verified ${links} local content links and fragments in generated documentation.`)
})

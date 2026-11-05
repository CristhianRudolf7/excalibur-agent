/** Discrete enhancements only: no scroll listener, animation loop or GSAP. */
const root = document.documentElement
const systemTheme = matchMedia('(prefers-color-scheme: dark)')
const reducedMotion = matchMedia('(prefers-reduced-motion: reduce)')
const themeButtons = document.querySelectorAll<HTMLButtonElement>('[data-theme-toggle]')

function preference() {
  try {
    return localStorage.getItem('starlight-theme')
  } catch {
    return null
  }
}
function syncTheme() {
  const dark = root.dataset.theme === 'dark'
  themeButtons.forEach((button) => {
    button.setAttribute(
      'aria-label',
      root.lang === 'es'
        ? `Usar tema ${dark ? 'claro' : 'oscuro'}`
        : `Switch to ${dark ? 'light' : 'dark'} theme`,
    )
  })
}
themeButtons.forEach((button) => {
  button.hidden = false
  button.addEventListener('click', () => {
    const theme = root.dataset.theme === 'dark' ? 'light' : 'dark'
    root.dataset.theme = theme
    try {
      localStorage.setItem('starlight-theme', theme)
    } catch {
      /* Storage can be unavailable. */
    }
    syncTheme()
  })
})
systemTheme.addEventListener('change', () => {
  if (!['light', 'dark'].includes(preference() ?? '')) {
    root.dataset.theme = systemTheme.matches ? 'dark' : 'light'
    syncTheme()
  }
})
window.addEventListener('storage', (event) => {
  if (event.key !== 'starlight-theme') return
  root.dataset.theme =
    event.newValue === 'light' || event.newValue === 'dark'
      ? event.newValue
      : systemTheme.matches
        ? 'dark'
        : 'light'
  syncTheme()
})
syncTheme()

const article = document.querySelector('main')
const toc = document.querySelector<HTMLElement>('[data-manual-toc]')
let offsets = new Map<HTMLAnchorElement, number>()
// Reads happen on arrival/resize/font reflow only, never while scrolling.
function measure() {
  if (article) root.style.setProperty('--article-height', `${article.offsetHeight}px`)
  if (toc) {
    const top = toc.getBoundingClientRect().top
    offsets = new Map(
      [...toc.querySelectorAll<HTMLAnchorElement>('a')].map((link) => [
        link,
        link.getBoundingClientRect().top - top + link.offsetHeight / 2 - 3.5,
      ]),
    )
    movePen()
  }
}
function movePen() {
  const current = toc?.querySelector<HTMLAnchorElement>('a[aria-current="true"]')
  if (current) toc?.style.setProperty('--toc-y', `${offsets.get(current) ?? 0}px`)
}
// Starlight's own IntersectionObserver remains the source of the current item.
const tocObserver = new MutationObserver(movePen)
if (toc)
  tocObserver.observe(toc, { attributes: true, attributeFilter: ['aria-current'], subtree: true })
const layoutObserver = new ResizeObserver(measure)
if (article) layoutObserver.observe(article)
if (toc) layoutObserver.observe(toc)
void document.fonts.ready.then(measure)
measure()
root.classList.add('manual-ready')

const headings = document.querySelectorAll('.sl-markdown-content h2')
const arrived = new IntersectionObserver(
  (entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue
      if (!reducedMotion.matches) entry.target.classList.add('manual-rule-arrived')
      arrived.unobserve(entry.target)
    }
  },
  { rootMargin: '0px 0px -12% 0px', threshold: 0 },
)
headings.forEach((heading) => arrived.observe(heading))

document.querySelectorAll<HTMLTableCellElement>('td').forEach((cell) => {
  if (/^[+−-]?[\d\s,.]+(?:%|\s?ms|\s?KB)?$/.test(cell.textContent?.trim() ?? ''))
    cell.dataset.numeric = ''
})

// Also give keyboard-shortcut users a predictable return point after search.
document.querySelector('site-search dialog')?.addEventListener('close', () => {
  document
    .querySelector<HTMLButtonElement>('site-search [data-open-modal]')
    ?.focus({ preventScroll: true })
})

import { ScrollTrigger } from './line'

type SectionModule = { init: (section: HTMLElement) => (() => void) | void }
const modules = import.meta.glob<SectionModule>('./sections/*.ts')
const disposers: (() => void)[] = []
let disposed = false

const root = document.documentElement
const buttons = [...document.querySelectorAll<HTMLButtonElement>('[data-theme-toggle]')]
const system = matchMedia('(prefers-color-scheme: dark)')
function storedTheme() {
  try {
    return localStorage.getItem('starlight-theme')
  } catch {
    return null
  }
}
function setTheme(theme: string | null) {
  const dark = theme === 'dark' || (theme !== 'light' && system.matches)
  root.dataset.theme = dark ? 'dark' : 'light'
  buttons.forEach((button) => {
    button.setAttribute('aria-label', `Switch to ${dark ? 'light' : 'dark'} theme`)
    const label = button.querySelector('[data-theme-label]')
    if (label) label.textContent = dark ? 'Light' : 'Dark'
  })
}
setTheme(storedTheme())
buttons.forEach((button) =>
  button.addEventListener('click', () => {
    const choice = root.dataset.theme === 'dark' ? 'light' : 'dark'
    try {
      localStorage.setItem('starlight-theme', choice)
    } catch {
      /* Storage is optional. */
    }
    setTheme(choice)
  }),
)
const systemChange = () => setTheme(storedTheme())
const storageChange = (event: StorageEvent) => {
  if (event.key === 'starlight-theme') setTheme(event.newValue)
}
system.addEventListener('change', systemChange)
window.addEventListener('storage', storageChange)
document
  .querySelectorAll('.mobile-menu a')
  .forEach((link) =>
    link.addEventListener('click', () => link.closest('details')?.removeAttribute('open')),
  )

const observer = new IntersectionObserver(
  (entries) => {
    for (const entry of entries) {
      if (!entry.isIntersecting) continue
      const section = entry.target as HTMLElement
      observer.unobserve(section)
      const name = section.dataset.section === 'top' ? 'hero' : section.dataset.section
      const loader = modules[`./sections/${name}.ts`]
      if (!loader) continue
      void loader()
        .then((module) => {
          if (disposed) return
          const dispose = module.init(section)
          if (dispose) disposers.push(dispose)
          section.dataset.initialized = 'true'
          root.dataset.landingReady = 'true'
        })
        .catch(() => {
          // Failed enhancements must never conceal the static page.
          root.classList.remove('anim')
        })
    }
  },
  { rootMargin: '100% 0px' },
)
document
  .querySelectorAll<HTMLElement>('[data-section]')
  .forEach((section) => observer.observe(section))
// Late font loading can affect phone heights. All geometry reads stay on refresh.
void document.fonts.ready.then(() => {
  if (!disposed) ScrollTrigger.refresh()
})
window.addEventListener(
  'pagehide',
  (event) => {
    // Preserve the live contexts when the browser freezes a back-forward entry.
    if (event.persisted) return
    disposed = true
    observer.disconnect()
    disposers.forEach((dispose) => dispose())
    system.removeEventListener('change', systemChange)
    window.removeEventListener('storage', storageChange)
  },
  { once: true },
)

window.addEventListener('pageshow', (event) => {
  if (event.persisted && !disposed) {
    setTheme(storedTheme())
    ScrollTrigger.refresh()
  }
})

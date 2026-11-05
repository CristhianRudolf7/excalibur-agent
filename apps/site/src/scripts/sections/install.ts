import { createSectionLine } from '../line'
import { prepareGeometry } from '../line/geometry'
import { reveal } from '../line/helpers'

export function init(section: HTMLElement) {
  const restore = prepareGeometry(section)
  const line = createSectionLine(section)
  const terminal = section.querySelector<HTMLElement>('[data-install-terminal]')!
  const tabs = [...section.querySelectorAll<HTMLAnchorElement>('[data-install-tab]')]
  const panels = [...section.querySelectorAll<HTMLElement>('[data-install-panel]')]
  const copy = section.querySelector<HTMLButtonElement>('[data-install-copy]')!
  const status = section.querySelector<HTMLElement>('[data-install-status]')!
  const controller = new AbortController()
  const options = { signal: controller.signal }
  let selected = 0
  let timer: ReturnType<typeof setTimeout> | undefined
  const select = (index: number, focus = false) => {
    selected = index
    tabs.forEach((tab, i) => {
      tab.setAttribute('aria-selected', String(i === index))
      tab.tabIndex = i === index ? 0 : -1
      panels[i]!.hidden = i !== index
    })
    copy.textContent = 'Copy command'
    status.textContent = ''
    if (focus) tabs[index]!.focus()
  }
  terminal.querySelector('nav')!.setAttribute('role', 'tablist')
  tabs.forEach((tab, i) => {
    tab.setAttribute('role', 'tab')
    tab.setAttribute('aria-controls', panels[i]!.id)
    panels[i]!.setAttribute('role', 'tabpanel')
    panels[i]!.setAttribute('aria-labelledby', tab.id)
    tab.addEventListener(
      'click',
      (event) => {
        event.preventDefault()
        select(i)
      },
      options,
    )
    tab.addEventListener(
      'keydown',
      (event) => {
        const next =
          event.key === 'ArrowRight'
            ? (i + 1) % tabs.length
            : event.key === 'ArrowLeft'
              ? (i + tabs.length - 1) % tabs.length
              : event.key === 'Home'
                ? 0
                : event.key === 'End'
                  ? tabs.length - 1
                  : event.key === ' '
                    ? i
                    : undefined
        if (next === undefined) return
        event.preventDefault()
        select(next, true)
      },
      options,
    )
  })
  const platform = navigator.userAgent
  select(/Windows/i.test(platform) ? 2 : /Macintosh|Mac OS X/i.test(platform) ? 1 : 0)
  terminal.classList.add('install-enhanced')
  copy.hidden = false
  copy.addEventListener(
    'click',
    () => {
      const text = panels[selected]!.querySelector('code')!.textContent!
      void navigator.clipboard
        .writeText(text)
        .then(() => {
          status.textContent = 'Command copied.'
          copy.textContent = 'Copied'
          clearTimeout(timer)
          timer = setTimeout(() => {
            copy.textContent = 'Copy command'
          }, 1800)
        })
        .catch(() => {
          status.textContent = 'Select the command and copy it with your keyboard.'
        })
    },
    options,
  )
  line.onReach(0.35, () => reveal(terminal))
  return () => {
    controller.abort()
    clearTimeout(timer)
    line.destroy()
    restore()
  }
}

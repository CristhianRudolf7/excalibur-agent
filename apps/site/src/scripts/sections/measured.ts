import { createSectionLine } from '../line'

export function init(section: HTMLElement) {
  const line = createSectionLine(section)
  return () => line.destroy()
}

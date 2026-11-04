import { useLabText, type LabMessage } from './i18n'

export function Tabs<T extends LabMessage>({
  label,
  values,
  value,
  onChange,
}: {
  label: string
  values: readonly T[]
  value: T
  onChange(value: NoInfer<T>): void
}) {
  const { text } = useLabText()
  return (
    <nav
      className="lab-tabs"
      role="tablist"
      aria-label={label}
      onKeyDown={(event) => {
        const buttons = [...event.currentTarget.querySelectorAll<HTMLButtonElement>('button')]
        const current = buttons.indexOf(document.activeElement as HTMLButtonElement)
        const index =
          event.key === 'Home'
            ? 0
            : event.key === 'End'
              ? buttons.length - 1
              : event.key === 'ArrowRight'
                ? (current + 1) % buttons.length
                : event.key === 'ArrowLeft'
                  ? (current - 1 + buttons.length) % buttons.length
                  : -1
        if (index >= 0) {
          event.preventDefault()
          buttons[index]?.focus()
          buttons[index]?.click()
        }
      }}
    >
      {values.map((item) => (
        <button
          key={item}
          role="tab"
          aria-selected={item === value}
          tabIndex={item === value ? 0 : -1}
          onClick={() => onChange(item)}
        >
          {text(item)}
        </button>
      ))}
    </nav>
  )
}

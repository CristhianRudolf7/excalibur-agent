import type { CSSProperties } from 'react'

export function Segmented<T extends string | number>({
  label,
  value,
  options,
  onChange,
}: {
  label: string
  value: T
  options: { value: T; label: string }[]
  onChange: (value: T) => void
}) {
  const index = options.findIndex((option) => option.value === value)
  return (
    <div
      className="settings-segmented"
      role="group"
      aria-label={label}
      style={{ '--segments': options.length, '--selected': index } as CSSProperties}
    >
      <span className="settings-segment-indicator" aria-hidden="true" />
      {options.map((option) => (
        <button
          key={option.value}
          type="button"
          aria-pressed={value === option.value}
          onClick={() => onChange(option.value)}
        >
          {option.label}
        </button>
      ))}
    </div>
  )
}

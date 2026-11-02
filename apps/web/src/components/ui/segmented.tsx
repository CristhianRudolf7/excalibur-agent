import type { CSSProperties } from 'react'
import { RadioGroup } from 'radix-ui'
import { cn } from '@/lib/utils'
import type { SelectOption } from './select'

/** A single-choice control with roving focus and arrow-key navigation. Pass translated labels. */
export function Segmented({
  label,
  value,
  onValueChange,
  options,
  disabled,
  className,
}: {
  label: string
  value: string
  onValueChange: (value: string) => void
  options: readonly SelectOption[]
  disabled?: boolean
  className?: string
}) {
  const index = options.findIndex((option) => option.value === value)
  return (
    <RadioGroup.Root
      className={cn('segmented', className)}
      aria-label={label}
      value={value}
      onValueChange={onValueChange}
      disabled={disabled}
      orientation="horizontal"
      style={
        { '--segments': options.length, '--segment-index': Math.max(index, 0) } as CSSProperties
      }
    >
      {index >= 0 && <span className="segmented-indicator" aria-hidden="true" />}
      {options.map((option) => (
        <RadioGroup.Item
          className="segmented-option"
          key={option.value}
          value={option.value}
          disabled={option.disabled}
        >
          {option.label}
        </RadioGroup.Item>
      ))}
    </RadioGroup.Root>
  )
}

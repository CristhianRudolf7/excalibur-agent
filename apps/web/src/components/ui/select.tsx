import { useState } from 'react'
import { Select as Listbox } from 'radix-ui'
import { Check, ChevronDown, ChevronUp } from 'lucide-react'
import { cn } from '@/lib/utils'

export interface SelectOption {
  value: string
  label: string
  disabled?: boolean
}

/** Labels are supplied by the caller after translation. Radix provides typeahead and focus management. */
export function Select({
  label,
  value,
  onValueChange,
  options,
  placeholder,
  disabled,
  name,
  className,
}: {
  label: string
  value?: string
  onValueChange: (value: string) => void
  options: readonly SelectOption[]
  placeholder?: string
  disabled?: boolean
  name?: string
  className?: string
}) {
  const [trigger, setTrigger] = useState<HTMLButtonElement | null>(null)
  return (
    <Listbox.Root value={value} onValueChange={onValueChange} disabled={disabled} name={name}>
      <Listbox.Trigger
        ref={setTrigger}
        className={cn('select-trigger', className)}
        aria-label={label}
      >
        <Listbox.Value placeholder={placeholder} />
        <Listbox.Icon>
          <ChevronDown aria-hidden="true" />
        </Listbox.Icon>
      </Listbox.Trigger>
      <Listbox.Portal container={trigger?.closest('dialog') ?? undefined}>
        <Listbox.Content
          className="select-popover"
          position="popper"
          sideOffset={6}
          collisionPadding={12}
        >
          <Listbox.ScrollUpButton className="select-scroll">
            <ChevronUp aria-hidden="true" />
          </Listbox.ScrollUpButton>
          <Listbox.Viewport>
            {options.map((option) => (
              <Listbox.Item
                key={option.value}
                value={option.value}
                disabled={option.disabled}
                className="select-option"
              >
                <Listbox.ItemText>{option.label}</Listbox.ItemText>
                <Listbox.ItemIndicator>
                  <Check aria-hidden="true" />
                </Listbox.ItemIndicator>
              </Listbox.Item>
            ))}
          </Listbox.Viewport>
          <Listbox.ScrollDownButton className="select-scroll">
            <ChevronDown aria-hidden="true" />
          </Listbox.ScrollDownButton>
        </Listbox.Content>
      </Listbox.Portal>
    </Listbox.Root>
  )
}

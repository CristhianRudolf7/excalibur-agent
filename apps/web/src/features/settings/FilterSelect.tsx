import { Select } from 'radix-ui'
import { Check, ChevronDown } from 'lucide-react'

/** Local settings listbox; Radix supplies typeahead and arrow-key navigation. */
export function FilterSelect({
  label,
  value,
  options,
  onChange,
}: {
  label: string
  value: string
  options: { value: string; label: string }[]
  onChange: (value: string) => void
}) {
  return (
    <Select.Root
      value={value || 'all'}
      onValueChange={(next) => onChange(next === 'all' ? '' : next)}
    >
      <Select.Trigger className="settings-filter-trigger" aria-label={label}>
        <span className="settings-filter-label">{label}:</span>
        <Select.Value />
        <Select.Icon>
          <ChevronDown size={15} />
        </Select.Icon>
      </Select.Trigger>
      <Select.Portal>
        <Select.Content
          className="settings-filter-popover"
          position="popper"
          sideOffset={6}
          collisionPadding={16}
        >
          <Select.Viewport>
            {options.map((option) => (
              <Select.Item
                key={option.value}
                value={option.value || 'all'}
                className="settings-filter-option"
              >
                <Select.ItemText>{option.label}</Select.ItemText>
                <Select.ItemIndicator>
                  <Check size={15} />
                </Select.ItemIndicator>
              </Select.Item>
            ))}
          </Select.Viewport>
        </Select.Content>
      </Select.Portal>
    </Select.Root>
  )
}

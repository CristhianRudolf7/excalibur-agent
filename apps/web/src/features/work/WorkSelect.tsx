import { useRef, useState } from 'react'
import { Select } from 'radix-ui'
import { Check, ChevronDown, Folder, Repeat2 } from 'lucide-react'

/** Local work-view listbox; kept inside dialogs so it shares their focus boundary. */
export function WorkSelect({
  label,
  value,
  onChange,
  options,
  icon = 'project',
}: {
  label: string
  value: string
  onChange: (value: string) => void
  options: { value: string; label: string }[]
  icon?: 'project' | 'repeat'
}) {
  const trigger = useRef<HTMLButtonElement>(null)
  const [boundary, setBoundary] = useState<Element | null>(null)
  // Radix reserves the empty value for a placeholder. Prefix every value bijectively.
  return (
    <Select.Root
      value={`value:${value}`}
      onValueChange={(next) => onChange(next.slice(6))}
      onOpenChange={(open) => {
        if (open) setBoundary(trigger.current?.closest('dialog') ?? null)
      }}
    >
      <Select.Trigger ref={trigger} className="work-select-trigger" aria-label={label}>
        {icon === 'project' ? <Folder aria-hidden="true" /> : <Repeat2 aria-hidden="true" />}
        <Select.Value />
        <Select.Icon>
          <ChevronDown />
        </Select.Icon>
      </Select.Trigger>
      <Select.Content
        className="work-select-menu"
        position="popper"
        sideOffset={6}
        collisionPadding={12}
        collisionBoundary={boundary ?? undefined}
      >
        <Select.Viewport>
          {options.map((option) => (
            <Select.Item
              className="work-select-option"
              key={option.value}
              value={`value:${option.value}`}
            >
              <Select.ItemText>{option.label}</Select.ItemText>
              <Select.ItemIndicator>
                <Check aria-hidden="true" />
              </Select.ItemIndicator>
            </Select.Item>
          ))}
        </Select.Viewport>
      </Select.Content>
    </Select.Root>
  )
}

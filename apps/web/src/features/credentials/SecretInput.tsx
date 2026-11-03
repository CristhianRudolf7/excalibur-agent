import { useState, type ComponentProps } from 'react'
import { useTranslation } from 'react-i18next'
import { Eye, EyeOff } from 'lucide-react'
import { Button } from '@/components/ui/button'

export function SecretInput({
  label,
  ...props
}: Omit<ComponentProps<'input'>, 'type'> & { label: string }) {
  const { t } = useTranslation()
  const [visible, setVisible] = useState(false)
  return (
    <label className="grid min-w-0 gap-2">
      {label}
      <span className="flex min-w-0 items-center gap-2">
        <input
          {...props}
          aria-label={label}
          type={visible ? 'text' : 'password'}
          className="min-w-0 flex-1 rounded-md border border-border bg-background px-3 py-2"
        />
        <Button
          variant="icon"
          aria-label={t(visible ? 'credentialHide' : 'credentialShow', { field: label })}
          aria-pressed={visible}
          disabled={props.disabled}
          onClick={() => setVisible(!visible)}
        >
          {visible ? <EyeOff aria-hidden="true" /> : <Eye aria-hidden="true" />}
        </Button>
      </span>
    </label>
  )
}

import type { ButtonHTMLAttributes } from 'react'
import { useTranslation } from 'react-i18next'

/**
 * Toggle for the left sidebar or the right panel. The glyph is a window
 * frame whose side panel slides in (open) or folds to a thin strip (closed),
 * so the button always shows the state it will change.
 */
export function PanelToggle({
  side,
  open,
  label,
  className = '',
  ...props
}: ButtonHTMLAttributes<HTMLButtonElement> & {
  side: 'left' | 'right'
  open: boolean
  label: string
}) {
  const { t } = useTranslation()
  return (
    <button
      type="button"
      aria-label={t(label)}
      title={t(label)}
      aria-expanded={open}
      data-side={side}
      data-open={open}
      className={`icon-button panel-toggle ${className}`}
      {...props}
    >
      <svg viewBox="0 0 20 20" width="18" height="18" aria-hidden="true">
        <rect className="pt-frame" x="2.25" y="3.25" width="15.5" height="13.5" rx="3.75" />
        <rect
          className="pt-pane"
          x={side === 'left' ? 4 : 11.5}
          y="5"
          width="4.5"
          height="10"
          rx="1.6"
        />
        <path
          className="pt-chevron"
          d={side === 'left' ? 'M12.4 8.2 10.6 10l1.8 1.8' : 'M7.6 8.2 9.4 10l-1.8 1.8'}
        />
      </svg>
    </button>
  )
}

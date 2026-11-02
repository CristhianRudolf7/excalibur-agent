import { Globe, MessageCircle } from 'lucide-react'
import { useTranslation } from 'react-i18next'
/** Controlled view selector. Its owner positions it and decides how a selected mode affects navigation. */
export function ModeSwitch({
  mode,
  onChange,
  variant = 'pill',
}: {
  mode: 'chat' | 'world'
  variant?: 'compact' | 'pill'
  onChange: (mode: 'chat' | 'world') => void
}) {
  const { t } = useTranslation()
  return (
    <div
      className={`mode-switch mode-switch-${variant}`}
      data-mode={mode}
      role="group"
      aria-label={t('worldMode')}
    >
      <button
        type="button"
        aria-label={t('worldChat')}
        title={`${t('worldChat')} (Ctrl+J)`}
        aria-pressed={mode === 'chat'}
        onClick={() => onChange('chat')}
      >
        <MessageCircle size={14} />
        {variant === 'pill' && t('worldChat')}
        <span className="mode-switch-tooltip" aria-hidden="true">
          {t('worldChat')} (Ctrl+J)
        </span>
      </button>
      <button
        type="button"
        aria-label={t(variant === 'compact' ? 'world' : 'worldModeLabel')}
        title={`${t('world')} (Ctrl+J)`}
        aria-pressed={mode === 'world'}
        onClick={() => onChange('world')}
      >
        <Globe size={14} />
        {variant === 'pill' && t('worldModeLabel')}
        <span className="mode-switch-tooltip" aria-hidden="true">
          {t('world')} (Ctrl+J)
        </span>
      </button>
    </div>
  )
}

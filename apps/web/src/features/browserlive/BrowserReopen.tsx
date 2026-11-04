import { Globe } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { openPanelView } from '@/features/files/open'
import { useBrowserLive } from './store'
import './browserlive.css'
export function BrowserReopen({
  conversation,
  visible,
}: {
  conversation: string
  visible: boolean
}) {
  const { t } = useTranslation()
  const active = useBrowserLive(
    (state) => state.conversations[conversation]?.session?.state === 'active',
  )
  if (!active || visible) return null
  return (
    <button
      className="browser-live-reopen"
      aria-label={t('browserReopen')}
      title={t('browserReopen')}
      onClick={() => openPanelView('browser')}
    >
      <Globe size={17} aria-hidden="true" />
      <i aria-hidden="true" />
    </button>
  )
}

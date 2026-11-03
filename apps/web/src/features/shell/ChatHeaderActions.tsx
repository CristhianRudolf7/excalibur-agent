import { CallOverlay } from '@/features/chat/voice/CallOverlay'
import { useCall } from '@/features/chat/voice/callStore'
import { DropdownMenu } from 'radix-ui'
import { MoreHorizontal, Phone } from 'lucide-react'
import { PanelToggle } from '@/components/ui/panel-toggle'
import { useTranslation } from 'react-i18next'
import { IconButton } from '@/components/shared'
import { ModeSwitch } from './ModeSwitch'

export function ChatHeaderActions({
  onMode,
  onPanel,
  panelOpen = false,
}: {
  onMode: (mode: 'chat' | 'world') => void
  /** Kept for callers; the info action was removed from the header. */
  onNotice?: (notice: 'phone' | 'info') => void
  onPanel: () => void
  panelOpen?: boolean
}) {
  const openCall = useCall((state) => state.open)
  const { t } = useTranslation()
  return (
    <div className="header-actions">
      <CallOverlay />
      <ModeSwitch variant="compact" mode="chat" onChange={onMode} />
      <span className="header-action-divider" aria-hidden="true" />
      <IconButton className="icon-button header-secondary-action" label="phone" onClick={openCall}>
        <Phone />
      </IconButton>
      <DropdownMenu.Root>
        <DropdownMenu.Trigger asChild>
          <IconButton className="icon-button header-overflow-trigger" label="more">
            <MoreHorizontal />
          </IconButton>
        </DropdownMenu.Trigger>
        <DropdownMenu.Portal>
          <DropdownMenu.Content className="header-overflow-menu" align="end" sideOffset={6}>
            <DropdownMenu.Item onSelect={openCall}>
              <Phone size={16} />
              {t('phone')}
            </DropdownMenu.Item>
          </DropdownMenu.Content>
        </DropdownMenu.Portal>
      </DropdownMenu.Root>
      <PanelToggle side="right" label="panel" open={panelOpen} onClick={onPanel} />
    </div>
  )
}

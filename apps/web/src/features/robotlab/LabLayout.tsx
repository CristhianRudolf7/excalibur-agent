import { useEffect, useRef, useState, type ReactNode } from 'react'
import { X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { ResizeHandle } from '@/components/resize/ResizeHandle'
import { useShell } from '@/features/shell/store'
import { splitWidths, splitRatioFromPanel } from '@/features/shell/split-layout'
import { Chat } from '@/features/chat/Chat'
import { useLabText } from './i18n'
import { useLab } from './state'

export function LabLayout({
  mobile,
  children,
  agentContent,
}: {
  mobile: boolean
  children: ReactNode
  agentContent?: ReactNode
}) {
  const { text } = useLabText()
  const shell = useShell()
  const agentOpen = useLab((state) => state.agentOpen)
  const split = useRef<HTMLDivElement>(null)
  const [width, setWidth] = useState(0)
  const dimensions = splitWidths(width, shell.ratio)
  const overlay = mobile || dimensions.overlay
  useEffect(() => {
    const node = split.current
    if (!node) return
    const observer = new ResizeObserver(([entry]) => setWidth(entry!.contentRect.width))
    observer.observe(node)
    return () => observer.disconnect()
  }, [])
  useEffect(() => {
    if (!agentOpen || !overlay) return
    const previous = document.activeElement
    split.current?.querySelector<HTMLButtonElement>('.lab-agent button')?.focus()
    return () => {
      if (previous instanceof HTMLElement && previous.isConnected) previous.focus()
    }
  }, [agentOpen, overlay])
  return (
    <div className="lab-body" ref={split}>
      <div className="lab-workspace" inert={agentOpen && overlay}>
        {children}
      </div>
      {agentOpen && (
        <>
          {!overlay && (
            <ResizeHandle
              label={text('resize')}
              value={dimensions.panel}
              min={320}
              max={Math.max(320, dimensions.available - 360)}
              defaultValue={dimensions.available / 2}
              direction={1}
              onReset={() => shell.setRatio(0.5)}
              onHome={() => shell.setRatio(0.5)}
              onChange={(value) => {
                const ratio = splitRatioFromPanel(value, width)
                if (ratio !== null) shell.setRatio(ratio)
              }}
              getPointerValue={(x) => {
                const rect = split.current?.getBoundingClientRect()
                return rect ? x - rect.left : null
              }}
            />
          )}
          <aside
            className={`lab-agent chat-column ${overlay ? 'lab-agent-overlay' : ''}`}
            aria-label={text('agent')}
            style={{ width: overlay ? '100%' : dimensions.chat }}
          >
            <header className="lab-agent-header">
              <strong>{text('agent')}</strong>
              <Button
                variant="icon"
                className="icon-button"
                aria-label={text('closeAgent')}
                onClick={() => useLab.getState().closeAgent()}
              >
                <X />
              </Button>
            </header>
            {agentContent ?? <Chat agentId="excalibur" onFile={() => {}} />}
          </aside>
        </>
      )}
    </div>
  )
}

import { useId, useState } from 'react'
import { ChevronDown, FileDiff } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { FileChange } from '@/lib/rpc/contract'
import { ChangeList } from '@/features/panel/ChangeList'
export { diffRows } from '@/features/panel/ChangeList'
export function ChangesCard({ changes, onFile }: { changes: FileChange[]; onFile: () => void }) {
  const { t } = useTranslation()
  const [collapsed, setCollapsed] = useState(false)
  const id = useId()
  const added = changes.reduce((sum, change) => sum + change.added, 0)
  const removed = changes.reduce((sum, change) => sum + change.removed, 0)
  return (
    <div className="changes-card">
      <button
        className="changes-header"
        aria-expanded={!collapsed}
        aria-controls={id}
        onClick={() => setCollapsed(!collapsed)}
      >
        <FileDiff size={16} aria-hidden="true" />
        <strong>{t('changesCount', { count: changes.length })}</strong>
        <span className="change-add">+{added}</span>
        <span className="change-del">−{removed}</span>
        <ChevronDown size={15} className={collapsed ? '' : 'expanded'} aria-hidden="true" />
      </button>
      {!collapsed && <ChangeList changes={changes} id={id} onOpen={onFile} />}
    </div>
  )
}

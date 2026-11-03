import { ChevronRight, FileDiff } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import type { FileChange } from '@/lib/rpc/contract'
import { openPanelView } from '@/features/files/open'
export { diffRows } from '@/features/panel/ChangeList'

/** Compact one-line summary of the files an assistant reply changed; the
 * expandable diffs live in the right panel's Changes view. */
export function ChangesCard({ changes }: { changes: FileChange[]; onFile?: () => void }) {
  const { t } = useTranslation()
  const added = changes.reduce((sum, change) => sum + change.added, 0)
  const removed = changes.reduce((sum, change) => sum + change.removed, 0)
  return (
    <button
      type="button"
      className="changes-line"
      onClick={() => openPanelView('changes')}
      aria-label={t('changesOpen', { count: changes.length })}
    >
      <FileDiff size={14} aria-hidden="true" />
      <span>{t('changesCount', { count: changes.length })}</span>
      <span className="change-add">+{added}</span>
      <span className="change-del">−{removed}</span>
      <span className="changes-line-cta">
        {t('changesView')}
        <ChevronRight size={14} aria-hidden="true" />
      </span>
    </button>
  )
}

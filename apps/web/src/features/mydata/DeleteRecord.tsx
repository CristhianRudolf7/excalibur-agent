import { notifyData } from './notice-store'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { useQueryClient } from '@tanstack/react-query'
import { Dialog } from '@/components/ui/dialog'
import { Button } from '@/components/ui/button'
import type { Collection, RecordRow } from '@/lib/rpc/contract'
import { rpc } from '@/lib/rpc/client'
import { dataKey } from './api'
export function DeleteRecord({
  collection,
  record,
  referenced,
  onClose,
  onDeleted,
}: {
  collection: Collection
  record: RecordRow
  referenced: boolean
  onClose: () => void
  onDeleted: () => void
}) {
  const { t } = useTranslation()
  const client = useQueryClient()
  const [busy, setBusy] = useState(false)
  const [failed, setFailed] = useState(false)
  return (
    <Dialog
      title="dataDelete"
      onClose={() => {
        if (!busy) onClose()
      }}
      footer={false}
    >
      <p>
        {t('dataDeleteQuestion', {
          name: String(record.values[collection.display_field] ?? record.id),
        })}
      </p>
      {referenced && <p role="note">{t('dataReferences')}</p>}
      {failed && <p role="alert">{t('dataActionError')}</p>}
      <div className="data-actions">
        <Button onClick={onClose} disabled={busy}>
          {t('dataCancel')}
        </Button>
        <Button
          variant="destructive"
          disabled={busy}
          onClick={async () => {
            setBusy(true)
            setFailed(false)
            try {
              await rpc.call('records.delete', { collection: collection.id, id: record.id })
              notifyData('dataDeleted')
              onDeleted()
              void client.invalidateQueries({
                queryKey: dataKey,
                predicate: (query) =>
                  !(
                    query.queryKey[1] === collection.id &&
                    query.queryKey[2] === 'record' &&
                    query.queryKey[3] === record.id
                  ),
              })
            } catch {
              setFailed(true)
              setBusy(false)
            }
          }}
        >
          {t('dataDelete')}
        </Button>
      </div>
    </Dialog>
  )
}

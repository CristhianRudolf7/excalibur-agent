import { useEffect } from 'react'
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { useNotice } from './notice-store'
export function DataNotice() {
  const { t } = useTranslation()
  const { message, set } = useNotice()
  useEffect(() => {
    if (!message) return
    const timer = setTimeout(() => set(''), 6000)
    return () => clearTimeout(timer)
  }, [message, set])
  return message ? (
    <div className="data-toast" role="status">
      {t(message)}
      <Button variant="ghost" aria-label={t('close')} onClick={() => set('')}>
        ×
      </Button>
    </div>
  ) : null
}

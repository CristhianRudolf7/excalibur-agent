import { useTranslation } from 'react-i18next'
import { displayProgress } from './logic'
export function Progress({ value, passed = false }: { value: number; passed?: boolean }) {
  const { t } = useTranslation()
  const progress = displayProgress(value, passed)
  return (
    <div className="classroom-progress">
      <progress max={100} value={progress} aria-label={t('classProgress', { value: progress })} />
      <span>{progress}%</span>
    </div>
  )
}

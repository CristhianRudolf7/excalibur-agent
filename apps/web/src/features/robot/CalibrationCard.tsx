import { useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Crosshair, Ruler, Trash2 } from 'lucide-react'
import type { useCalibration } from './useCalibration'

export function CalibrationCard({
  calibration,
  onToggle,
}: {
  calibration: ReturnType<typeof useCalibration>
  onToggle: () => void
}) {
  const { t } = useTranslation()
  const [confirming, setConfirming] = useState(false)
  const [page, setPage] = useState(0)
  const { query, mutation, available, marking } = calibration
  const status = query.data
  const points = status?.points ?? []
  const lastPage = Math.max(0, Math.ceil(points.length / 8) - 1)
  const currentPage = Math.min(page, lastPage)
  const disabled = !available || mutation.isPending
  const error = mutation.error?.message ?? query.error?.message
  return (
    <section
      className="robot-card robot-calibration-card"
      aria-labelledby="robot-calibration-title"
    >
      <div className="robot-card-heading">
        <span className="robot-card-icon">
          <Crosshair aria-hidden="true" />
        </span>
        <div>
          <h2 id="robot-calibration-title">{t('robotCalibration')}</h2>
          <p>{t('robotCalibrationHint')}</p>
        </div>
      </div>
      <div
        className={`robot-calibration-status ${status?.calibrated ? 'is-calibrated' : ''}`}
        role="status"
      >
        <span>
          <i />
          {t(
            query.isPending
              ? 'robotCalibrationLoading'
              : status?.calibrated
                ? 'robotCalibrated'
                : 'robotNotCalibrated',
          )}
        </span>
        <dl>
          <div>
            <dt>{t('robotCalibrationPoints')}</dt>
            <dd>{t('robotCalibrationCount', { count: points.length })}</dd>
          </div>
          <div>
            <dt>{t('robotCalibrationError')}</dt>
            <dd>
              {status?.calibrated
                ? t('robotCalibrationPixels', { value: status.reprojection_error_px.toFixed(2) })
                : '—'}
            </dd>
          </div>
          <div>
            <dt>{t('robotCalibrationHeight')}</dt>
            <dd>
              {status ? t('robotCalibrationMeters', { value: status.table_z.toFixed(3) }) : '—'}
            </dd>
          </div>
        </dl>
      </div>
      <ol className="robot-calibration-steps" id="robot-calibration-steps">
        {[1, 2, 3, 4, 5].map((step) => (
          <li key={step}>{t(`robotCalibrationStep${step}`)}</li>
        ))}
      </ol>
      <div className="robot-calibration-actions">
        <button
          type="button"
          className={`robot-button ${marking ? '' : 'robot-primary'}`}
          aria-pressed={marking}
          aria-describedby="robot-calibration-steps"
          disabled={disabled && !marking}
          onClick={onToggle}
        >
          <Crosshair aria-hidden="true" />
          {t(marking ? 'robotCalibrationExit' : 'robotCalibrationMark')}
        </button>
        <button
          type="button"
          className="robot-button"
          disabled={disabled}
          onClick={calibration.setHeight}
        >
          <Ruler aria-hidden="true" />
          {t('robotCalibrationSetHeight')}
        </button>
        <button
          type="button"
          className="robot-button"
          disabled={disabled}
          onClick={() => setConfirming(true)}
        >
          <Trash2 aria-hidden="true" />
          {t('robotCalibrationClear')}
        </button>
      </div>
      {!available && <p className="robot-field-hint">{t('robotCalibrationUnavailable')}</p>}
      {marking && (
        <p className="robot-calibration-hint" role="status">
          {t('robotCalibrationMarkHint')}
        </p>
      )}
      {confirming && available && (
        <div
          className="robot-calibration-confirm"
          role="group"
          aria-label={t('robotCalibrationClear')}
        >
          <p>{t('robotCalibrationConfirm')}</p>
          <button
            type="button"
            className="robot-button"
            disabled={disabled}
            onClick={() => {
              setConfirming(false)
              calibration.clear()
            }}
          >
            {t('robotCalibrationConfirmClear')}
          </button>
          <button type="button" className="robot-button" onClick={() => setConfirming(false)}>
            {t('robotCalibrationCancel')}
          </button>
        </div>
      )}
      {error && (
        <div className="robot-calibration-failure" role="alert">
          <p className="robot-error">{error}</p>
          <button
            type="button"
            className="robot-button"
            disabled={query.isFetching || mutation.isPending}
            onClick={() => {
              mutation.reset()
              void query.refetch()
            }}
          >
            {t('robotCalibrationRetry')}
          </button>
        </div>
      )}
      {status?.error && <p className="robot-field-hint">{status.error}</p>}
      {points.length > 0 && (
        <div className="robot-calibration-points">
          <h3>{t('robotCalibrationPointList')}</h3>
          <ol start={currentPage * 8 + 1}>
            {points.slice(currentPage * 8, currentPage * 8 + 8).map((point, index) => (
              <li key={currentPage * 8 + index}>
                <span>
                  {t('robotCalibrationPixelPair', {
                    u: point.pixel[0].toFixed(1),
                    v: point.pixel[1].toFixed(1),
                  })}
                </span>
                <span aria-hidden="true">→</span>
                <strong>
                  {t('robotCalibrationBasePair', {
                    x: point.base[0].toFixed(3),
                    y: point.base[1].toFixed(3),
                  })}
                </strong>
              </li>
            ))}
          </ol>
          {lastPage > 0 && (
            <div className="robot-calibration-actions">
              <button
                type="button"
                className="robot-button"
                disabled={currentPage === 0}
                onClick={() => setPage(currentPage - 1)}
              >
                {t('robotCalibrationPrevious')}
              </button>
              <button
                type="button"
                className="robot-button"
                disabled={currentPage === lastPage}
                onClick={() => setPage(currentPage + 1)}
              >
                {t('robotCalibrationNext')}
              </button>
            </div>
          )}
        </div>
      )}
    </section>
  )
}

import { useCallback, useEffect, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { Select } from 'radix-ui'
import { Camera, Check, ChevronDown, Video, VideoOff } from 'lucide-react'
import { cameraErrorKey, RobotCamera } from './camera'
import type { RobotImageSize } from '@/lib/rpc/contract'
import { CalibrationCard } from './CalibrationCard'
import { previewPixel } from './calibration'
import { useCalibration } from './useCalibration'

export function CameraCard({ connected = false }: { connected?: boolean }) {
  const { t } = useTranslation()
  const video = useRef<HTMLVideoElement>(null)
  const [devices, setDevices] = useState<MediaDeviceInfo[]>([])
  const [device, setDevice] = useState('default')
  const [phase, setPhase] = useState<'off' | 'starting' | 'on'>('off')
  const [interval, setIntervalSeconds] = useState(2)
  const [lastSent, setLastSent] = useState<number | null>(null)
  const [now, setNow] = useState(Date.now)
  const [error, setError] = useState<string | null>(null)
  const [frame, setFrame] = useState<RobotImageSize | null>(null)
  const calibration = useCalibration(connected && phase === 'on' && frame !== null)
  const [camera] = useState(
    () =>
      new RobotCamera({
        sent: (at, image) => {
          setFrame(image)
          setLastSent(at)
          setNow(at)
          setError(null)
        },
        error: setError,
        ended: () => {
          setFrame(null)
          setPhase('off')
          setError('robotCameraEnded')
        },
      }),
  )
  const attempt = useRef(0)
  const cancel = useCallback(() => {
    ++attempt.current
    camera.stop()
  }, [camera])
  useEffect(() => {
    let mounted = true
    const refresh = async () => {
      try {
        const list = await navigator.mediaDevices?.enumerateDevices()
        if (mounted) setDevices((list ?? []).filter((entry) => entry.kind === 'videoinput'))
      } catch {
        // Device enumeration can be withheld until camera permission is granted.
      }
    }
    void refresh()
    navigator.mediaDevices?.addEventListener('devicechange', refresh)
    return () => {
      mounted = false
      navigator.mediaDevices?.removeEventListener('devicechange', refresh)
      cancel()
    }
  }, [cancel])
  useEffect(() => {
    if (lastSent === null) return
    const timer = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(timer)
  }, [lastSent])

  const stop = () => {
    cancel()
    setFrame(null)
    setPhase('off')
  }
  const start = async () => {
    if (!video.current) return
    const current = ++attempt.current
    setPhase('starting')
    setError(null)
    setLastSent(null)
    setFrame(null)
    try {
      const started = await camera.start(video.current, device === 'default' ? '' : device)
      if (current !== attempt.current || !started) return
      setPhase('on')
      try {
        const list = await navigator.mediaDevices.enumerateDevices()
        if (current === attempt.current)
          setDevices(list.filter((entry) => entry.kind === 'videoinput'))
      } catch {
        // The selected stream still works if device labels cannot be refreshed.
      }
    } catch (failure) {
      if (current !== attempt.current) return
      setPhase('off')
      setError(cameraErrorKey(failure))
    }
  }
  const active = phase === 'on'
  const saved = calibration.query.data
  const sameImage =
    frame && saved?.image.width === frame.width && saved.image.height === frame.height
  return (
    <>
      <section className="robot-card robot-camera-card" aria-labelledby="robot-camera-title">
        <div className="robot-card-heading">
          <span className="robot-card-icon">
            <Camera aria-hidden="true" />
          </span>
          <div>
            <h2 id="robot-camera-title">{t('robotCamera')}</h2>
            <p>{t('robotCameraHint')}</p>
          </div>
          {active && (
            <span className="robot-live">
              <i />
              {t('robotCameraLive')}
            </span>
          )}
        </div>
        <div className="robot-camera-controls">
          <Select.Root value={device} onValueChange={setDevice} disabled={phase !== 'off'}>
            <Select.Trigger className="robot-device" aria-label={t('robotCameraDevice')}>
              <Video aria-hidden="true" />
              <Select.Value />
              <Select.Icon>
                <ChevronDown />
              </Select.Icon>
            </Select.Trigger>
            <Select.Portal>
              <Select.Content className="robot-device-menu" position="popper" sideOffset={6}>
                <Select.Viewport>
                  {[
                    { value: 'default', label: t('robotCameraDefault') },
                    ...devices
                      .filter((entry) => entry.deviceId && entry.deviceId !== 'default')
                      .map((entry, index) => ({
                        value: entry.deviceId,
                        label: entry.label || t('robotCameraNumber', { number: index + 1 }),
                      })),
                  ].map(({ value, label }) => (
                    <Select.Item key={value} value={value} className="robot-device-option">
                      <Select.ItemText>{label}</Select.ItemText>
                      <Select.ItemIndicator>
                        <Check />
                      </Select.ItemIndicator>
                    </Select.Item>
                  ))}
                </Select.Viewport>
              </Select.Content>
            </Select.Portal>
          </Select.Root>
          <button
            type="button"
            className={`robot-button ${phase === 'off' ? 'robot-primary' : ''}`}
            onClick={() => (phase === 'off' ? void start() : stop())}
          >
            {phase === 'off' ? <Video aria-hidden="true" /> : <VideoOff aria-hidden="true" />}
            {t(phase === 'off' ? 'robotCameraStart' : 'robotCameraStop')}
          </button>
        </div>
        <div
          className={`robot-preview ${active ? 'is-live' : ''} ${calibration.marking ? 'is-marking' : ''}`}
        >
          <video
            ref={video}
            aria-label={t('robotCameraPreview')}
            autoPlay
            playsInline
            muted
            onResize={() => setFrame(null)}
          />
          {calibration.marking && frame && (
            <>
              <button
                type="button"
                className="robot-calibration-capture"
                aria-label={t('robotCalibrationCapture')}
                disabled={calibration.mutation.isPending}
                onClick={(event) => {
                  if (!video.current) return
                  const rect = video.current.getBoundingClientRect()
                  const pixel = previewPixel(
                    event.detail === 0 ? rect.left + rect.width / 2 : event.clientX,
                    event.detail === 0 ? rect.top + rect.height / 2 : event.clientY,
                    rect,
                    frame,
                  )
                  if (pixel) calibration.addPoint(pixel)
                }}
              />
              {sameImage && (
                <svg
                  className="robot-calibration-markers"
                  viewBox={`0 0 ${frame.width} ${frame.height}`}
                  aria-hidden="true"
                >
                  {saved.points.map((point, index) => (
                    <g key={index} transform={`translate(${point.pixel[0]} ${point.pixel[1]})`}>
                      <circle r={frame.width * 0.025} />
                      <text
                        textAnchor="middle"
                        dominantBaseline="central"
                        fontSize={frame.width * 0.03}
                      >
                        {index + 1}
                      </text>
                    </g>
                  ))}
                </svg>
              )}
            </>
          )}
          {!active && (
            <div className="robot-preview-empty">
              <span>
                <Camera aria-hidden="true" />
              </span>
              <strong>{t(phase === 'starting' ? 'robotCameraStarting' : 'robotCameraOff')}</strong>
              <p>{t('robotCameraOffHint')}</p>
            </div>
          )}
        </div>
        {calibration.marking && (
          <div className="robot-calibration-preview-hint">
            <span role="status">
              {t(
                calibration.mutation.isPending
                  ? 'robotCalibrationSaving'
                  : 'robotCalibrationMarkHint',
              )}
            </span>
            <button type="button" className="robot-button" onClick={calibration.exit}>
              {t('robotCalibrationExit')}
            </button>
          </div>
        )}
        <div className="robot-frame-controls">
          <div>
            <span id="robot-interval-label">{t('robotInterval')}</span>
            <div className="robot-interval" role="group" aria-labelledby="robot-interval-label">
              {[1, 2, 5].map((seconds) => (
                <button
                  type="button"
                  key={seconds}
                  aria-pressed={interval === seconds}
                  onClick={() => {
                    setIntervalSeconds(seconds)
                    camera.setInterval(seconds * 1000)
                  }}
                >
                  {t('robotSeconds', { count: seconds })}
                </button>
              ))}
            </div>
          </div>
          <p className="robot-frame-age" role="status">
            {lastSent !== null
              ? t('robotLastFrame', { count: Math.max(0, Math.floor((now - lastSent) / 1000)) })
              : active
                ? t('robotFrameWaiting')
                : null}
          </p>
        </div>
        {error && (
          <p className="robot-error" role="alert">
            {t(error)}
          </p>
        )}
        <p className="robot-camera-privacy">{t('robotCameraPrivacy')}</p>
      </section>
      <CalibrationCard
        calibration={calibration}
        onToggle={() => {
          calibration.toggle()
          if (!calibration.marking) video.current?.scrollIntoView({ block: 'center' })
        }}
      />
    </>
  )
}

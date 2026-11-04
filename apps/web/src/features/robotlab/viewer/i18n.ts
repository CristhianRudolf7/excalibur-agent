import { useTranslation } from 'react-i18next'
import i18n from '@/lib/i18n'

const en = {
  recenter: 'Recenter',
  webglUnavailable: 'The 3D view is unavailable. WebGL is required.',
  noScene: 'Waiting for the scene…',
  loadingScene: 'Loading scene…',
  noCamera: 'Waiting for the robot camera…',
  meshError: 'Some robot meshes could not be loaded.',
  sceneError: 'The scene could not be loaded.',
  replayError: 'The recording could not be loaded.',
  loading: 'Loading recording…',
  play: 'Play',
  pause: 'Pause',
  replay: 'Replay',
  seek: 'Replay position',
  speed: 'Playback speed',
}
const es: typeof en = {
  recenter: 'Recentrar',
  webglUnavailable: 'La vista 3D no está disponible. Se requiere WebGL.',
  noScene: 'Esperando la escena…',
  loadingScene: 'Cargando escena…',
  noCamera: 'Esperando la cámara del robot…',
  meshError: 'No se pudieron cargar algunas mallas del robot.',
  sceneError: 'No se pudo cargar la escena.',
  replayError: 'No se pudo cargar la grabación.',
  loading: 'Cargando grabación…',
  play: 'Reproducir',
  pause: 'Pausar',
  replay: 'Repetición',
  seek: 'Posición de la repetición',
  speed: 'Velocidad de reproducción',
}
i18n.addResourceBundle('en', 'robotlabViewer', en, true, true)
i18n.addResourceBundle('es', 'robotlabViewer', es, true, true)
export function useViewerText() {
  const { t } = useTranslation('robotlabViewer')
  return (key: keyof typeof en) => t(key)
}

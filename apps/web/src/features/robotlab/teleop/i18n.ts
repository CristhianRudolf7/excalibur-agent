import { useTranslation } from 'react-i18next'
const messages = {
  title: ['Webcam teleoperation', 'Teleoperación por cámara'],
  privacy: [
    'Video stays on this device. Only body and hand points are sent to the simulation.',
    'El vídeo permanece en este dispositivo. Solo se envían puntos del cuerpo y las manos a la simulación.',
  ],
  start: ['Start camera', 'Iniciar cámara'],
  stop: ['Stop', 'Detener'],
  retry: ['Retry', 'Reintentar'],
  camera: ['Camera', 'Cámara'],
  arm: ['Arm', 'Brazo'],
  left: ['Left', 'Izquierdo'],
  right: ['Right', 'Derecho'],
  mirror: ['Mirror preview', 'Reflejar vista'],
  smoothing: ['Smoothing', 'Suavizado'],
  calibrate: ['Calibrate neutral pose', 'Calibrar postura neutra'],
  calibrated: ['Neutral pose captured', 'Postura neutra capturada'],
  hold: ['Require hold to move', 'Mantener pulsado para mover'],
  move: ['Hold to move (Space)', 'Mantén pulsado para mover (Espacio)'],
  ready: ['Tracking', 'Seguimiento'],
  waiting: ['Waiting for confident tracking', 'Esperando seguimiento fiable'],
  preparing: ['Preparing camera', 'Preparando cámara'],
  mapping: ['View/edit mapping code', 'Ver/editar código de mapeo'],
  defaultMapping: ['Scenario default mapping', 'Mapeo predeterminado del escenario'],
  customMapping: ['Agent/user mapping', 'Mapeo del agente/usuario'],
  save: ['Save mapping', 'Guardar mapeo'],
  reset: ['Use scenario default', 'Usar predeterminado'],
  close: ['Close editor', 'Cerrar editor'],
  unknownMapping: [
    'Current mapping code is not available from the service. Saving replaces the active mapping.',
    'El servicio no proporciona el código actual. Al guardar se reemplaza el mapeo activo.',
  ],
  vision: ['What the agent sees', 'Lo que ve el agente'],
  unavailable: ['Vision unavailable', 'Visión no disponible'],
  instruction: ['Instruction', 'Instrucción'],
  run: ['Run step', 'Ejecutar paso'],
  see: ['See', 'Ver'],
  think: ['Think', 'Pensar'],
  act: ['Act', 'Actuar'],
  simulationVerified: ['Verified by simulation', 'Verificado por simulación'],
  modelOpinion: ['Model opinion', 'Opinión del modelo'],
  verify: ['Verify', 'Verificar'],
  history: ['Previous steps', 'Pasos anteriores'],
  none: ['No action chosen', 'Ninguna acción elegida'],
  idle: ['Idle', 'En reposo'],
  confidence: ['Confidence', 'Confianza'],
  disabled: ['Camera teleoperation is disabled', 'Teleoperación por cámara desactivada'],
  visionDisabled: ['Vision panel is disabled', 'Panel de visión desactivado'],
} as const
export function useRobotText() {
  const { t, i18n } = useTranslation()
  return (key: keyof typeof messages) =>
    t(`robotlabCapture.${key}`, {
      defaultValue: messages[key][(i18n.language ?? 'en').startsWith('es') ? 1 : 0],
    })
}

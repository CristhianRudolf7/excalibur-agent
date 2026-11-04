import { expect, it } from 'vitest'
import i18n from '@/lib/i18n'
import { activityLabel } from './activityLabel'
import type { BrowserActivityEvent } from './types'
const action: BrowserActivityEvent = {
  conversation_id: 'c',
  session_id: 's',
  run_id: 'r',
  seq: 1,
  kind: 'click',
  phase: 'start',
  target: 'Continue',
  url: 'https://example.test/page',
}
for (const language of ['en', 'es']) {
  it(`labels every action in ${language}, keeping password targets out`, async () => {
    await i18n.changeLanguage(language)
    const t = i18n.getFixedT(language)
    const kinds = [
      'navigate',
      'click',
      'type',
      'type_password',
      'read',
      'wait',
      'select',
      'hover',
      'scroll',
      'press',
      'upload',
      'drag',
      'download',
    ]
    const expected =
      language === 'en'
        ? [
            'Opening example.test',
            "Click on 'Continue'",
            "Typing in 'Continue'",
            'Typing password',
            'Reading the page',
            'Waiting for the page',
            "Selecting in 'Continue'",
            "Hovering 'Continue'",
            'Scrolling the page',
            'Pressing a key',
            'Uploading a file',
            'Dragging',
            'Downloading a file',
          ]
        : [
            'Abriendo example.test',
            "Clic en 'Continue'",
            "Escribiendo en 'Continue'",
            'Escribiendo contraseña',
            'Leyendo la página',
            'Esperando a que cargue',
            "Seleccionando en 'Continue'",
            "Pasando sobre 'Continue'",
            'Desplazando la página',
            'Pulsando tecla',
            'Subiendo archivo',
            'Arrastrando',
            'Descargando archivo',
          ]
    expect(kinds.map((kind) => activityLabel({ ...action, kind }, t))).toEqual(expected)
    expect(activityLabel({ ...action, kind: 'type_password' }, t)).not.toContain('Continue')
    expect(activityLabel({ ...action, target: undefined }, t)).toBe(
      language === 'en' ? 'Click' : 'Clic',
    )
    expect(activityLabel({ ...action, kind: 'type', target: undefined }, t)).toBe(
      language === 'en' ? 'Typing' : 'Escribiendo',
    )
    expect(activityLabel({ ...action, kind: 'page' }, t)).toBe('')
    expect(activityLabel({ ...action, kind: 'navigate', url: '' }, t)).toBe(
      language === 'en' ? 'Opening page' : 'Abriendo página',
    )
  })
}

import { describe, expect, it } from 'vitest'
import { collapseRepeats, isEcho, sessionTranscript, speakableText, stripEcho } from './speech'

describe('speakableText', () => {
  it('reads markdown as natural sentences without marks, bullets or emoji', () => {
    const md = [
      '## Resumen 🚀',
      '',
      'Esto es **importante** y _urgente_ ✅.',
      '',
      '- Primer punto',
      '* Segundo punto 👉 con `código`',
      '1. Paso uno',
      '- [x] Tarea hecha',
      '> Nota: cita',
      '',
      '---',
      'Ver [la guía](https://example.com/a_(b)) o https://example.com/x [1].',
      '',
      '```bash',
      'rm -rf /tmp/x',
      '```',
      '',
      '| Categoría | Monto |',
      '| :--- | ---: |',
      '| Diseño | 980 € |',
      '',
      'A → B — fin • listo',
    ].join('\n')
    expect(speakableText(md)).toBe(
      'Resumen. Esto es importante y urgente. Primer punto. Segundo punto con código. Paso uno. ' +
        'Tarea hecha. Nota: cita. Ver la guía o. Categoría, Monto. Diseño, 980 €. A, B, fin listo.',
    )
  })
  it('keeps ordinary words, numbers and identifiers intact', () => {
    expect(speakableText('El valor es -5, el archivo snake_case_name y 3.14 grados.')).toBe(
      'El valor es -5, el archivo snake_case_name y 3.14 grados.',
    )
    expect(speakableText('¿Listo? ¡Vamos!')).toBe('¿Listo? ¡Vamos!')
    expect(speakableText('🎉🎉')).toBe('')
  })
})

describe('recognition transcripts', () => {
  const results = (...texts: string[]) =>
    texts.map((transcript) => ({ isFinal: true, 0: { transcript } }))
  it('collapses engines that resend growing prefixes as new results', () => {
    expect(
      sessionTranscript(
        results(
          'tengo',
          'tengo',
          'tengo algún',
          'tengo algún listado',
          'tengo algún listado de',
          'tengo algún listado de tarea',
          'tengo algún listado de tarea pendiente',
          'tengo algún listado de tarea pendiente',
        ),
      ),
    ).toBe('tengo algún listado de tarea pendiente')
  })
  it('keeps distinct consecutive segments from desktop engines', () => {
    expect(sessionTranscript(results('hola', 'qué tal estás', 'muy bien'))).toBe(
      'hola qué tal estás muy bien',
    )
  })
  it('collapses repeats across restarted sessions too', () => {
    expect(collapseRepeats(['abre el chat', 'abre el chat y busca', 'noticias'])).toBe(
      'abre el chat y busca noticias',
    )
  })
})

describe('assistant echo during playback', () => {
  const spoken = 'Respuesta uno. Con dos frases sobre el clima de hoy.'
  it('ignores the assistant voice picked up by the microphone', () => {
    expect(isEcho('respuesta uno con dos frases', spoken)).toBe(true)
    expect(isEcho('el clima', spoken)).toBe(true)
    expect(isEcho('', spoken)).toBe(true)
  })
  it('treats new words from the user as a barge-in', () => {
    expect(isEcho('espera quiero preguntarte otra cosa', spoken)).toBe(false)
    expect(isEcho('hola qué tal', '')).toBe(false)
  })
  it('strips the echoed lead-in from the user turn', () => {
    expect(stripEcho('respuesta uno con espera quiero otra cosa', spoken)).toBe(
      'espera quiero otra cosa',
    )
    expect(stripEcho('hola', '')).toBe('hola')
  })
})

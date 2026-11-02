import { afterEach, expect, it } from 'vitest'
import { cleanup, render, screen } from '@testing-library/react'
import '@/lib/i18n'
import { panels } from '@/features/shell/panels'
import { libraryEnabled } from './api'
import { Composer } from '@/features/chat/Composer'
import MarkdownBody from '@/components/markdown/MarkdownBody'
import { seedSources } from './seed'
import { Library } from './Library'

afterEach(cleanup)
it('honors the build-time library switch across the panel, attachments and citations', () => {
  // The library tab was removed from the right panel by request.
  expect((panels as readonly string[]).includes('library')).toBe(false)
  const { unmount } = render(
    <Composer agent="Byte" typing={false} onSend={() => {}} onStop={() => {}} />,
  )
  expect(
    (screen.getByRole('button', { name: 'Adjuntar archivo' }) as HTMLButtonElement).disabled,
  ).toBe(!libraryEnabled)
  unmount()
  render(<MarkdownBody sources={seedSources}>{'Claim [1]'}</MarkdownBody>)
  expect(screen.queryByRole('button', { name: 'Fuente 1' }) !== null).toBe(libraryEnabled)
  expect(screen.queryByRole('button', { name: 'Fuentes (2)' }) !== null).toBe(libraryEnabled)
  if (!libraryEnabled) {
    const panel = render(<Library />)
    expect(panel.container).toBeEmptyDOMElement()
  }
})

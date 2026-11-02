import { render, screen, cleanup } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { DocumentViewer } from './DocumentViewer'
import { useViewerStore } from './store'
import i18n from '@/lib/i18n'
vi.mock('./switches', () => ({ viewerEnabled: false }))
afterEach(() => {
  cleanup()
  vi.unstubAllGlobals()
  useViewerStore.getState().close()
})
it('does not fetch or render previews when the switch is off', () => {
  void i18n.changeLanguage('en')
  const fetch = vi.fn()
  vi.stubGlobal('fetch', fetch)
  useViewerStore.getState().open({ documentId: 'real-document' })
  render(<DocumentViewer />)
  expect(screen.getByText('Document previews are disabled.')).toBeInTheDocument()
  expect(screen.queryByRole('button', { name: 'PDF' })).not.toBeInTheDocument()
  expect(fetch).not.toHaveBeenCalled()
})

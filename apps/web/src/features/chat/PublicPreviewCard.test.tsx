import { act, cleanup, fireEvent, render, screen } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import i18n from '@/lib/i18n'
import { PublicPreviewCard, PublicPreviewMessage } from './PublicPreviewCard'
import { extractPublicPreviews } from './public-preview'

const preview = {
  url: 'https://small-phone-preview.trycloudflare.com',
  expiresAt: '2027-01-01T12:00:00Z',
}
const markdown = `[Public preview](${preview.url} "public-preview:${preview.expiresAt}")`

beforeEach(async () => {
  await i18n.changeLanguage('en')
})
afterEach(() => {
  cleanup()
  vi.useRealTimers()
  vi.restoreAllMocks()
})

describe('public preview card', () => {
  it('opens the published URL in a new tab and shows its expiry', () => {
    render(<PublicPreviewCard preview={preview} />)
    expect(screen.getByRole('group', { name: 'Public preview' })).toBeInTheDocument()
    const link = screen.getByRole('link', { name: preview.url })
    expect(link).toHaveAttribute('href', preview.url)
    expect(link).toHaveAttribute('target', '_blank')
    expect(link).toHaveAttribute('rel', 'noopener noreferrer')
    expect(screen.getByText(/^Expires:/)).toHaveAttribute('dateTime', preview.expiresAt)
  })
  it('copies the URL and announces success', async () => {
    const writeText = vi.fn().mockResolvedValue(undefined)
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } })
    render(<PublicPreviewCard preview={preview} />)
    await act(async () => fireEvent.click(screen.getByRole('button', { name: 'Copy public link' })))
    expect(writeText).toHaveBeenCalledWith(preview.url)
    expect(screen.getByRole('status')).toHaveTextContent('Link copied')
  })
  it('reports clipboard failure in Spanish', async () => {
    await i18n.changeLanguage('es')
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { writeText: vi.fn().mockRejectedValue(new Error('denied')) },
    })
    render(<PublicPreviewCard preview={preview} />)
    expect(screen.getByRole('group', { name: 'Vista previa pública' })).toBeInTheDocument()
    expect(screen.getByText(/^Caduca:/)).toBeInTheDocument()
    await act(async () =>
      fireEvent.click(screen.getByRole('button', { name: 'Copiar enlace público' })),
    )
    expect(screen.getByRole('alert')).toHaveTextContent('No se pudo copiar el enlace.')
  })
  it('marks the card expired when the TTL elapses', () => {
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2027-01-01T11:59:59Z'))
    render(<PublicPreviewCard preview={preview} />)
    expect(screen.getByText(/^Expires:/)).toBeInTheDocument()
    act(() => vi.advanceTimersByTime(1000))
    expect(screen.getByText(/^Expired:/)).toBeInTheDocument()
  })
  it('renders a persisted assistant message as a card without duplicating the metadata link', () => {
    render(<PublicPreviewMessage>{markdown}</PublicPreviewMessage>)
    expect(screen.getAllByRole('link')).toHaveLength(1)
    expect(screen.queryByText(/public-preview:/)).not.toBeInTheDocument()
  })
  it('preserves Markdown indentation in ordinary messages and preview replies', () => {
    const text = '    console.log("hello")\n\nA code example.'
    expect(extractPublicPreviews(text)).toEqual({ body: text, previews: [] })
    expect(extractPublicPreviews(`${text}\n\n${markdown}`)).toEqual({
      body: text,
      previews: [preview],
    })
  })
  it('extracts only explicit preview links and deduplicates them', () => {
    expect(extractPublicPreviews(`Done.\n\n${markdown}\n${markdown}`)).toEqual({
      body: 'Done.',
      previews: [preview],
    })
    for (const text of [
      preview.url,
      markdown.replace('trycloudflare.com', 'trycloudflare.com.evil.test'),
      markdown.replace(preview.expiresAt, 'invalid'),
      markdown.replace('https:', 'javascript:'),
    ]) {
      expect(extractPublicPreviews(text)).toEqual({ body: text, previews: [] })
    }
  })
})

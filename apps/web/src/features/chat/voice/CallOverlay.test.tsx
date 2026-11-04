import { act, cleanup, render } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import '@/lib/i18n'
import { Composer } from '../Composer'
import { CallOverlay } from './CallOverlay'
import { useCall } from './callStore'

vi.mock('@/components/ui/dialog', () => ({
  Dialog: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
}))

const initial = useCall.getState()
afterEach(() => {
  cleanup()
  useCall.setState(initial)
})

it('formats assistant captions while preserving transcripts, user lines and partials', () => {
  const transcript = [
    { role: 'assistant' as const, text: '**Tasks**: [Review](https://example.test)' },
  ]
  useCall.setState({
    overlay: true,
    snapshot: { state: 'listening', energy: 0, transcript },
  })
  const { container } = render(
    <>
      <Composer agent="Byte" typing={false} onSend={() => {}} onStop={() => {}} />
      <CallOverlay />
    </>,
  )
  const compact = () => container.querySelector('.composer-call-caption')
  const overlay = () => document.querySelector('.call-caption p:not([hidden])')
  expect(compact()).toHaveTextContent('Tasks: Review.')
  expect(overlay()).toHaveTextContent('Tasks: Review.')
  expect(useCall.getState().snapshot.transcript).toBe(transcript)
  expect(transcript[0]!.text).toBe('**Tasks**: [Review](https://example.test)')

  act(() => {
    useCall.setState({
      snapshot: { state: 'listening', energy: 0, transcript, partial: '**partial**' },
    })
  })
  expect(compact()).toHaveTextContent('**partial**')
  expect(overlay()).toHaveTextContent('**partial**')

  act(() => {
    useCall.setState({
      snapshot: { state: 'listening', energy: 0, transcript: [{ role: 'user', text: '**user**' }] },
    })
  })
  expect(compact()).toHaveTextContent('**user**')
  expect(overlay()).toHaveTextContent('**user**')
})

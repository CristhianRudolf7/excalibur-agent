import { fireEvent, render, screen } from '@testing-library/react'
import { useState } from 'react'
import { expect, it, vi } from 'vitest'
import '@/lib/i18n'
import { ErrorBoundary } from './ErrorScreen'

function Bomb({ explode }: { explode: boolean }) {
  if (explode) throw new TypeError('boom in view')
  return <p>healthy view</p>
}

function Harness() {
  const [explode, setExplode] = useState(true)
  const [path, setPath] = useState('/a')
  return (
    <>
      <button onClick={() => setExplode(false)}>fix</button>
      <button onClick={() => setPath('/b')}>navigate</button>
      <ErrorBoundary resetKey={path}>
        <Bomb explode={explode} />
      </ErrorBoundary>
    </>
  )
}

it('shows the branded error view with details and recovers on retry and navigation', () => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
  render(<Harness />)
  expect(screen.getByRole('alert')).toHaveTextContent('Esta vista tuvo un problema')
  fireEvent.click(screen.getByRole('button', { name: 'Detalles técnicos' }))
  expect(screen.getByText(/TypeError: boom in view/)).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'fix' }))
  fireEvent.click(screen.getByRole('button', { name: 'Reintentar' }))
  expect(screen.getByText('healthy view')).toBeInTheDocument()
})

it('clears the error when the route changes', () => {
  vi.spyOn(console, 'error').mockImplementation(() => {})
  function Nav() {
    const [path, setPath] = useState('/a')
    return (
      <>
        <button onClick={() => setPath('/b')}>navigate</button>
        <ErrorBoundary resetKey={path}>
          {path === '/a' ? <Bomb explode /> : <p>other page</p>}
        </ErrorBoundary>
      </>
    )
  }
  render(<Nav />)
  expect(screen.getByRole('alert')).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'navigate' }))
  expect(screen.getByText('other page')).toBeInTheDocument()
})

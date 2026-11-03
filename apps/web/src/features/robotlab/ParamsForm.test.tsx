import { render, screen, fireEvent } from '@testing-library/react'
import { expect, it, vi } from 'vitest'
import { ParamsForm } from './ParamsForm'
it('generates numeric and checkbox controls with typed values', () => {
  const change = vi.fn()
  render(
    <ParamsForm
      schema={{
        properties: {
          links: { type: 'integer', minimum: 1, maximum: 3 },
          randomize: { type: 'boolean' },
        },
      }}
      value={{ links: 1, randomize: false }}
      onChange={change}
    />,
  )
  const links = screen.getByRole('spinbutton', { name: 'links' })
  expect(links).toHaveAttribute('min', '1')
  expect(links).toHaveAttribute('max', '3')
  fireEvent.change(links, { target: { value: '3' } })
  expect(change).toHaveBeenLastCalledWith({ links: 3, randomize: false })
  fireEvent.click(screen.getByRole('checkbox', { name: 'randomize' }))
  expect(change).toHaveBeenLastCalledWith({ links: 1, randomize: true })
})
